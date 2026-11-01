package reconcile

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	networkingv1ac "k8s.io/client-go/applyconfigurations/networking/v1"

	"github.com/MrDHat/noma/internal/store"
)

// Labels on every object the reconciler creates.
const (
	labelManaged   = "noma.dev/managed"
	labelOwner     = "noma.dev/owner"
	labelWorkspace = "noma.dev/workspace"
)

// Workspace pod settings for M2. The image is a placeholder until the
// workspace image exists.
const (
	workspaceImage  = "registry.k8s.io/pause:3.10.1"
	workspaceUID    = 1000
	workspaceMemory = "512Mi"
	workspaceCPU    = "500m"
	volumeSize      = "1Gi"
	runtimeClass    = "gvisor"
)

// namespaceFor returns the namespace holding a user's workspaces.
func namespaceFor(owner string) string { return "noma-u-" + owner }

func managedLabels(owner string) map[string]string {
	return map[string]string{labelManaged: "true", labelOwner: owner}
}

func workspaceLabels(ws *store.Workspace) map[string]string {
	l := managedLabels(ws.Owner)
	l[labelWorkspace] = ws.ID
	return l
}

func namespace(owner string) *corev1ac.NamespaceApplyConfiguration {
	l := managedLabels(owner)
	l["pod-security.kubernetes.io/enforce"] = "restricted"
	return corev1ac.Namespace(namespaceFor(owner)).WithLabels(l)
}

// networkPolicy denies all traffic to and from the user's workspaces except
// DNS, noma-server and the public internet.
func networkPolicy(owner string) *networkingv1ac.NetworkPolicyApplyConfiguration {
	udp, tcp := corev1.ProtocolUDP, corev1.ProtocolTCP
	dns := intstr.FromInt32(53)
	return networkingv1ac.NetworkPolicy("workspaces", namespaceFor(owner)).
		WithLabels(managedLabels(owner)).
		WithSpec(networkingv1ac.NetworkPolicySpec().
			WithPodSelector(metav1ac.LabelSelector()).
			WithPolicyTypes(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress).
			WithEgress(
				networkingv1ac.NetworkPolicyEgressRule().
					WithTo(networkingv1ac.NetworkPolicyPeer().
						WithNamespaceSelector(metav1ac.LabelSelector().WithMatchLabels(map[string]string{"kubernetes.io/metadata.name": "kube-system"})).
						WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(map[string]string{"k8s-app": "kube-dns"}))).
					WithPorts(
						networkingv1ac.NetworkPolicyPort().WithProtocol(udp).WithPort(dns),
						networkingv1ac.NetworkPolicyPort().WithProtocol(tcp).WithPort(dns)),
				networkingv1ac.NetworkPolicyEgressRule().
					WithTo(networkingv1ac.NetworkPolicyPeer().
						WithNamespaceSelector(metav1ac.LabelSelector().WithMatchLabels(map[string]string{"kubernetes.io/metadata.name": "noma-system"})).
						WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(map[string]string{"app": "noma-server"}))),
				networkingv1ac.NetworkPolicyEgressRule().
					WithTo(networkingv1ac.NetworkPolicyPeer().
						WithIPBlock(networkingv1ac.IPBlock().
							WithCIDR("0.0.0.0/0").
							WithExcept("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "100.64.0.0/10"))),
			))
}

func serviceAccount(ws *store.Workspace) *corev1ac.ServiceAccountApplyConfiguration {
	return corev1ac.ServiceAccount(ws.ID, namespaceFor(ws.Owner)).
		WithLabels(workspaceLabels(ws)).
		WithAutomountServiceAccountToken(false)
}

func volumeClaim(ws *store.Workspace) *corev1ac.PersistentVolumeClaimApplyConfiguration {
	return corev1ac.PersistentVolumeClaim(ws.ID, namespaceFor(ws.Owner)).
		WithLabels(workspaceLabels(ws)).
		WithSpec(corev1ac.PersistentVolumeClaimSpec().
			WithAccessModes(corev1.ReadWriteOnce).
			WithResources(corev1ac.VolumeResourceRequirements().
				WithRequests(corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(volumeSize)})))
}

// statefulSet runs the workspace pod: at most one, under gVisor, locked down.
func statefulSet(ws *store.Workspace) *appsv1ac.StatefulSetApplyConfiguration {
	limits := corev1.ResourceList{
		corev1.ResourceMemory: resource.MustParse(workspaceMemory),
		corev1.ResourceCPU:    resource.MustParse(workspaceCPU),
	}
	return appsv1ac.StatefulSet(ws.ID, namespaceFor(ws.Owner)).
		WithLabels(workspaceLabels(ws)).
		WithSpec(appsv1ac.StatefulSetSpec().
			WithReplicas(1).
			WithServiceName(ws.ID).
			WithSelector(metav1ac.LabelSelector().WithMatchLabels(map[string]string{labelWorkspace: ws.ID})).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithLabels(workspaceLabels(ws)).
				WithSpec(corev1ac.PodSpec().
					WithRuntimeClassName(runtimeClass).
					WithServiceAccountName(ws.ID).
					WithAutomountServiceAccountToken(false).
					WithEnableServiceLinks(false).
					WithSecurityContext(corev1ac.PodSecurityContext().
						WithRunAsNonRoot(true).
						WithRunAsUser(workspaceUID).
						WithRunAsGroup(workspaceUID).
						WithFSGroup(workspaceUID).
						WithSeccompProfile(corev1ac.SeccompProfile().WithType(corev1.SeccompProfileTypeRuntimeDefault))).
					WithContainers(corev1ac.Container().
						WithName("workspace").
						WithImage(workspaceImage).
						WithResources(corev1ac.ResourceRequirements().WithRequests(limits).WithLimits(limits)).
						WithSecurityContext(corev1ac.SecurityContext().
							WithAllowPrivilegeEscalation(false).
							WithReadOnlyRootFilesystem(true).
							WithCapabilities(corev1ac.Capabilities().WithDrop("ALL"))).
						WithVolumeMounts(corev1ac.VolumeMount().WithName("workspace").WithMountPath("/workspace"))).
					WithVolumes(corev1ac.Volume().
						WithName("workspace").
						WithPersistentVolumeClaim(corev1ac.PersistentVolumeClaimVolumeSource().WithClaimName(ws.ID))))))
}
