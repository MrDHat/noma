package preflight

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// egressComponent labels the pod the NetworkPolicy check denies egress to.
const egressComponent = "preflight-egress"

// checkNetworkPolicy denies a pod all egress and waits for it to confirm it
// lost its route to the Kubernetes API. A cluster whose network plugin
// ignores NetworkPolicy fails here: workspace isolation would be off.
func checkNetworkPolicy(ctx context.Context, kube kubernetes.Interface, o Options) error {
	policies := kube.NetworkingV1().NetworkPolicies(o.Namespace)
	policy, err := policies.Create(ctx, &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "noma-preflight-deny-egress-"},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
				"app.kubernetes.io/component": egressComponent,
			}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create the NetworkPolicy check's policy: %w", err)
	}
	defer func() {
		_ = policies.Delete(context.Background(), policy.Name, metav1.DeleteOptions{})
	}()
	return runPod(ctx, kube, o, egressComponent, "egress-blocked",
		"a pod denied all egress by a NetworkPolicy could still reach the Kubernetes API: the cluster's network plugin must enforce NetworkPolicy")
}

// EgressBlocked waits up to wait for this pod to lose its route to the
// Kubernetes API, as the NetworkPolicy check's policy should make it. A
// policy can take a moment to apply to a new pod, so it needs several
// failed connections in a row.
func EgressBlocked(wait time.Duration) bool {
	addr := net.JoinHostPort(os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT"))
	failed := 0
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			if failed++; failed == 3 {
				return true
			}
			continue
		}
		_ = conn.Close()
		failed = 0
		time.Sleep(time.Second)
	}
	return false
}
