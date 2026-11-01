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
	Store *store.Store
	Kube  kubernetes.Interface

	wake chan struct{}
}

// New returns a reconciler. Call Run to start it.
func New(st *store.Store, kube kubernetes.Interface) *Reconciler {
	return &Reconciler{Store: st, Kube: kube, wake: make(chan struct{}, 1)}
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
		if err := r.pass(ctx); err != nil && ctx.Err() == nil {
			slog.Error("reconcile pass failed", "err", err)
			time.AfterFunc(retryAfter, r.Wake)
		}
	}
}

// pass reconciles every workspace once.
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

// ensure applies every object a running workspace needs and records what it sees.
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
			_, err := r.Kube.CoreV1().ServiceAccounts(ns).Apply(ctx, serviceAccount(ws), applyOpts)
			return err
		},
		func() error {
			_, err := r.Kube.CoreV1().PersistentVolumeClaims(ns).Apply(ctx, volumeClaim(ws), applyOpts)
			return err
		},
		func() error {
			_, err := r.Kube.AppsV1().StatefulSets(ns).Apply(ctx, statefulSet(ws), applyOpts)
			return err
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			_ = r.Store.SetObserved(ctx, ws.ID, api.PhaseError, "could not create workspace objects")
			return err
		}
	}
	phase, msg, err := r.observe(ctx, ws)
	if err != nil {
		return err
	}
	if phase != ws.Phase || msg != ws.Message {
		return r.Store.SetObserved(ctx, ws.ID, phase, msg)
	}
	return nil
}

// observe turns the workspace pod's state into a phase and message.
// The agent reports readiness itself once it runs (M2.5).
func (r *Reconciler) observe(ctx context.Context, ws *store.Workspace) (string, string, error) {
	pod, err := r.Kube.CoreV1().Pods(namespaceFor(ws.Owner)).Get(ctx, ws.ID+"-0", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return api.PhaseStarting, "creating pod", nil
	}
	if err != nil {
		return "", "", err
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if w := cs.State.Waiting; w != nil && w.Reason != "ContainerCreating" {
			return api.PhaseStarting, w.Reason, nil
		}
	}
	return api.PhaseStarting, "pod " + string(pod.Status.Phase), nil
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
