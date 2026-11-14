// Package preflight checks that a cluster has what Noma needs before the
// chart installs it (M3.5-15). The chart never creates these itself: the
// RuntimeClass and StorageClasses belong to whoever runs the cluster.
package preflight

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

// Options are what the install needs from the cluster.
type Options struct {
	// RuntimeClass is the gVisor RuntimeClass workspaces run under.
	RuntimeClass string
	// StorageClasses are the classes the install names; "" stands for the
	// cluster's default class.
	StorageClasses []string
	// Namespace and Image are where and what the sandbox pod runs: the
	// noma-server image, whose sandboxed command checks it is in gVisor.
	Namespace, Image string
	// Timeout bounds the wait for the sandbox pod.
	Timeout time.Duration
}

// Check returns an error naming every prerequisite that is missing.
func Check(ctx context.Context, kube kubernetes.Interface, o Options) error {
	var missing []string
	_, err := kube.NodeV1().RuntimeClasses().Get(ctx, o.RuntimeClass, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		missing = append(missing, fmt.Sprintf("RuntimeClass %q does not exist: install gVisor on the nodes and create it", o.RuntimeClass))
	case err != nil:
		return fmt.Errorf("read RuntimeClass %q: %w", o.RuntimeClass, err)
	}
	for _, class := range o.StorageClasses {
		msg, err := checkStorageClass(ctx, kube, class)
		if err != nil {
			return err
		}
		if msg != "" {
			missing = append(missing, msg)
		}
	}
	if len(missing) > 0 {
		return errors.New("the cluster is missing what Noma needs:\n  - " + strings.Join(missing, "\n  - "))
	}
	return checkSandbox(ctx, kube, o)
}

func checkStorageClass(ctx context.Context, kube kubernetes.Interface, class string) (string, error) {
	if class != "" {
		_, err := kube.StorageV1().StorageClasses().Get(ctx, class, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return fmt.Sprintf("StorageClass %q does not exist", class), nil
		}
		return "", err
	}
	list, err := kube.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("list StorageClasses: %w", err)
	}
	for _, sc := range list.Items {
		if sc.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" {
			return "", nil
		}
	}
	return "no StorageClass is the cluster's default: mark one as default or name one in the chart's values", nil
}

// checkSandbox runs one pod under the RuntimeClass and waits for it to
// confirm it is sandboxed: a RuntimeClass whose handler is missing or is
// not gVisor fails here.
func checkSandbox(ctx context.Context, kube kubernetes.Interface, o Options) error {
	pods := kube.CoreV1().Pods(o.Namespace)
	pod, err := pods.Create(ctx, sandboxPod(o), metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("start the sandbox check pod: %w", err)
	}
	defer func() {
		_ = pods.Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
	}()
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	for {
		p, err := pods.Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("a pod under RuntimeClass %q did not finish within %v: %w", o.RuntimeClass, o.Timeout, err)
		}
		switch p.Status.Phase {
		case corev1.PodSucceeded:
			return nil
		case corev1.PodFailed:
			return fmt.Errorf("a pod under RuntimeClass %q is not sandboxed by gVisor", o.RuntimeClass)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("a pod under RuntimeClass %q did not finish within %v (phase %s): is gVisor installed on the nodes?",
				o.RuntimeClass, o.Timeout, p.Status.Phase)
		case <-time.After(time.Second):
		}
	}
}

func sandboxPod(o Options) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "noma-preflight-sandbox-",
			Labels:       map[string]string{"app.kubernetes.io/name": "noma", "app.kubernetes.io/component": "preflight"},
		},
		Spec: corev1.PodSpec{
			RuntimeClassName:             &o.RuntimeClass,
			RestartPolicy:                corev1.RestartPolicyNever,
			AutomountServiceAccountToken: ptr.To(false),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot:   ptr.To(true),
				SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			Containers: []corev1.Container{{
				Name:    "sandboxed",
				Image:   o.Image,
				Command: []string{"/noma-server", "sandboxed"},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					ReadOnlyRootFilesystem:   ptr.To(true),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
		},
	}
}
