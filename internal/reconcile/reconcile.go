// Package reconcile drives Kubernetes towards the workspaces stored in
// Postgres. Postgres is the source of truth; every step is a server-side
// apply or delete, so running it again is always safe.
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/store"
)

const (
	fieldManager = "noma-server"
	resync       = 5 * time.Minute
	retryAfter   = 10 * time.Second
)

var applyOpts = metav1.ApplyOptions{FieldManager: fieldManager, Force: true}

// Reconciler creates and deletes workspace objects.
type Reconciler struct {
	Store  *store.Store
	Kube   kubernetes.Interface
	Config Config

	wake chan struct{}
}

// New returns a reconciler. Call Run to start it.
func New(st *store.Store, kube kubernetes.Interface, cfg Config) *Reconciler {
	return &Reconciler{Store: st, Kube: kube, Config: cfg, wake: make(chan struct{}, 1)}
}

// Wake asks for a pass soon. It never blocks.
func (r *Reconciler) Wake() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run reconciles until ctx is done. It runs a pass when woken by the API,
// when one of our objects changes, after a failed pass, and every 5 minutes.
func (r *Reconciler) Run(ctx context.Context) {
	factory := informers.NewSharedInformerFactoryWithOptions(r.Kube, resync,
		informers.WithTweakListOptions(func(o *metav1.ListOptions) { o.LabelSelector = labelManaged + "=true" }))
	onChange := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { r.Wake() },
		UpdateFunc: func(any, any) { r.Wake() },
		DeleteFunc: func(any) { r.Wake() },
	}
	for _, inf := range []cache.SharedIndexInformer{
		factory.Apps().V1().StatefulSets().Informer(),
		factory.Core().V1().PersistentVolumeClaims().Informer(),
		factory.Core().V1().Pods().Informer(),
	} {
		if _, err := inf.AddEventHandler(onChange); err != nil {
			slog.Error("add informer handler", "err", err)
		}
	}
	factory.Start(ctx.Done())

	ticker := time.NewTicker(resync)
	defer ticker.Stop()
	r.Wake()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-ticker.C:
		}
		err := r.Store.WithReconcileLock(ctx, func() error { return r.pass(ctx) })
		if err == nil || ctx.Err() != nil {
			continue
		}
		if errors.Is(err, store.ErrLocked) {
			// The other replica may have read the rows before what woke us.
			slog.Info("reconcile pass skipped; retrying", "reason", err)
		} else {
			slog.Error("reconcile pass failed", "err", err)
		}
		time.AfterFunc(retryAfter, r.Wake)
	}
}

// pass reconciles every workspace once. Run holds the reconcile lock around
// it: two replicas in one pass at once (as during a rollout) could apply a
// workspace's objects again after the other deleted its row.
func (r *Reconciler) pass(ctx context.Context) error {
	workspaces, err := r.Store.Workspaces(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for i := range workspaces {
		ws := &workspaces[i]
		var err error
		if ws.Desired == store.DesiredDeleted {
			err = r.remove(ctx, ws)
		} else {
			err = r.ensure(ctx, ws)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("workspace %s: %w", ws.ID, err))
		}
	}
	return errors.Join(errs...)
}

// ensure applies every object a workspace needs and records what it sees. A
// stopped workspace keeps its ServiceAccount and volume, with no pod.
func (r *Reconciler) ensure(ctx context.Context, ws *store.Workspace) error {
	ns := namespaceFor(ws.Owner)
	steps := []func() error{
		func() error {
			_, err := r.Kube.CoreV1().Namespaces().Apply(ctx, namespace(ws.Owner), applyOpts)
			return err
		},
		func() error {
			_, err := r.Kube.NetworkingV1().NetworkPolicies(ns).Apply(ctx, networkPolicy(ws.Owner), applyOpts)
			return err
		},
		func() error {
			_, err := r.Kube.CoreV1().ConfigMaps(ns).Apply(ctx, caConfigMap(ws.Owner, r.Config.AgentCA), applyOpts)
			return err
		},
		func() error {
			_, err := r.Kube.CoreV1().ServiceAccounts(ns).Apply(ctx, serviceAccount(ws), applyOpts)
			return err
		},
		func() error {
			_, err := r.Kube.CoreV1().PersistentVolumeClaims(ns).Apply(ctx, volumeClaim(ws), applyOpts)
			return err
		},
		func() error {
			_, err := r.Kube.AppsV1().StatefulSets(ns).Apply(ctx, statefulSet(ws, r.Config), applyOpts)
			return err
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			// Once the agent has reported, its pod is running and its
			// phase stays: a failed apply does not stop the workspace.
			if reconcilerPhase(ws) {
				_ = r.setPhase(ctx, ws, api.PhaseError, errObjects)
			}
			return err
		}
	}
	return r.observe(ctx, ws)
}

// errObjects is the message of the error phase ensure sets.
const errObjects = "could not create workspace objects"

// reconcilerPhase reports whether ws's phase is one the reconciler set,
// rather than one the agent reported.
func reconcilerPhase(ws *store.Workspace) bool {
	switch ws.Phase {
	case api.PhasePending, api.PhaseStarting, api.PhaseStopping, api.PhaseStopped:
		return true
	case api.PhaseError:
		return ws.Message == errObjects
	}
	return false
}

// observe records the workspace pod's state while it starts. Once the pod
// runs, the agent reports the phase; observe then only replaces a phase the
// reconciler set, such as an error from a failed apply that has since
// succeeded. Every write is conditional on the row being as this pass read
// it, so observe never overwrites what the agent reported in the meantime.
func (r *Reconciler) observe(ctx context.Context, ws *store.Workspace) error {
	pod, err := r.Kube.CoreV1().Pods(namespaceFor(ws.Owner)).Get(ctx, ws.ID+"-0", metav1.GetOptions{})
	if ws.Desired == store.DesiredStopped {
		switch {
		case apierrors.IsNotFound(err):
			return r.setPhase(ctx, ws, api.PhaseStopped, "")
		case err != nil:
			return err
		}
		// The pod's deletion wakes the next pass.
		return r.setPhase(ctx, ws, api.PhaseStopping, "")
	}
	if apierrors.IsNotFound(err) {
		return r.setStarting(ctx, ws, "creating pod")
	}
	if err != nil {
		return err
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil && w.Reason != "ContainerCreating" {
			return r.setStarting(ctx, ws, w.Reason)
		}
	}
	if pod.Status.Phase != corev1.PodRunning {
		return r.setStarting(ctx, ws, "pod "+string(pod.Status.Phase))
	}
	if reconcilerPhase(ws) {
		return r.setStarting(ctx, ws, "starting agent")
	}
	return nil
}

func (r *Reconciler) setStarting(ctx context.Context, ws *store.Workspace, msg string) error {
	return r.setPhase(ctx, ws, api.PhaseStarting, msg)
}

// setPhase writes a phase unless ws already has it, and only if the row
// has not changed since this pass read it.
func (r *Reconciler) setPhase(ctx context.Context, ws *store.Workspace, phase, msg string) error {
	if ws.Phase == phase && ws.Message == msg {
		return nil
	}
	return r.Store.SetPhaseIfUnchanged(ctx, ws, phase, msg)
}

// remove deletes a workspace's objects, then its row once they are gone.
func (r *Reconciler) remove(ctx context.Context, ws *store.Workspace) error {
	ns := namespaceFor(ws.Owner)
	bg := metav1.DeletePropagationBackground
	opts := metav1.DeleteOptions{PropagationPolicy: &bg}
	deletes := []func() error{
		func() error { return r.Kube.AppsV1().StatefulSets(ns).Delete(ctx, ws.ID, opts) },
		func() error { return r.Kube.CoreV1().PersistentVolumeClaims(ns).Delete(ctx, ws.ID, opts) },
		func() error { return r.Kube.CoreV1().ServiceAccounts(ns).Delete(ctx, ws.ID, opts) },
	}
	for _, del := range deletes {
		if err := del(); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	// The pod and volume take a moment to go; an informer event wakes us
	// when they do.
	if gone, err := r.gone(ctx, ws); err != nil || !gone {
		return err
	}
	if err := r.Store.DeleteWorkspace(ctx, ws.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	slog.Info("workspace deleted", "id", ws.ID, "name", ws.Name)
	return nil
}

func (r *Reconciler) gone(ctx context.Context, ws *store.Workspace) (bool, error) {
	ns := namespaceFor(ws.Owner)
	_, err := r.Kube.CoreV1().Pods(ns).Get(ctx, ws.ID+"-0", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	_, err = r.Kube.CoreV1().PersistentVolumeClaims(ns).Get(ctx, ws.ID, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	return true, nil
}
