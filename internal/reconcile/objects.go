package reconcile

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsv1ac "k8s.io/client-go/applyconfigurations/apps/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	networkingv1ac "k8s.io/client-go/applyconfigurations/networking/v1"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/store"
)

// Labels on every object the reconciler creates.
const (
	labelManaged   = "noma.dev/managed"
	labelOwner     = "noma.dev/owner"
	labelWorkspace = "noma.dev/workspace"
)

// Workspace pod settings for M2.
const (
	workspaceUID  = 1000
	tokenPath     = "/var/run/secrets/noma"
	tokenLifetime = 3600
)

// Config is how the reconciler builds workspace pods.
type Config struct {
	// Image is the workspace image, with noma-agent as its entrypoint.
	Image string
	// ServerURL is noma-server's in-cluster agent endpoint.
	ServerURL string
	// Resources are each workspace container's requests and limits
	// (M3.5-7).
	Resources corev1.ResourceRequirements
	// RuntimeClass is the gVisor RuntimeClass workspace pods run under.
	RuntimeClass string
	// DiskSize is each workspace volume's size (M3.5-23).
	DiskSize resource.Quantity
	// StorageClass is the workspace volumes' StorageClass; "" is the
	// cluster's default.
	StorageClass string
	// LoginStorageClass and LoginAccessMode are for each user's login
	// claim, which all their workspaces mount: ReadWriteMany once
	// workspaces run on several nodes, as on EFS (M3.5-20).
	LoginStorageClass string
	LoginAccessMode   corev1.PersistentVolumeAccessMode
	// AgentCA is the PEM CA that signs the agent endpoint's certificate.
	// Workspaces get it from a ConfigMap, so published images work with
	// any cluster's CA.
	AgentCA string
	// ClaudeProfileURL overrides Claude's profile endpoint for the account
	// check; CI points it at noma-server's fake one.
	ClaudeProfileURL string
}

// workspaceDir is where a workspace's volume is mounted. Each workspace has
// its own path, because Claude keys its per-project state by path.
func workspaceDir(ws *store.Workspace) string { return "/workspaces/" + ws.ID }

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
// DNS, noma-server's agent API and the public internet.
func networkPolicy(owner string) *networkingv1ac.NetworkPolicyApplyConfiguration {
	udp, tcp := corev1.ProtocolUDP, corev1.ProtocolTCP
	dns, agentAPI := intstr.FromInt32(53), intstr.FromInt32(8443)
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
						WithPodSelector(metav1ac.LabelSelector().WithMatchLabels(map[string]string{"app": "noma-server"}))).
					WithPorts(networkingv1ac.NetworkPolicyPort().WithProtocol(tcp).WithPort(agentAPI)),
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

func volumeClaim(ws *store.Workspace, cfg Config) *corev1.PersistentVolumeClaim {
	return claim(ws.ID, namespaceFor(ws.Owner), workspaceLabels(ws), cfg.StorageClass,
		corev1.ReadWriteOnce, cfg.DiskSize)
}

// claim is a PersistentVolumeClaim. The reconciler only ever creates
// claims: Kubernetes refuses most changes to one, so a changed size or
// class applies to new workspaces and leaves existing volumes alone.
func claim(name, ns string, labels map[string]string, class string,
	mode corev1.PersistentVolumeAccessMode, size resource.Quantity) *corev1.PersistentVolumeClaim {
	c := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{mode},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: size},
			},
		},
	}
	if class != "" {
		c.Spec.StorageClassName = &class
	}
	return c
}

// caConfigMap holds the CA workspaces trust for noma-server's agent
// endpoint, in the user's namespace. It is per owner and never deleted.
func caConfigMap(owner, pem string) *corev1ac.ConfigMapApplyConfiguration {
	return corev1ac.ConfigMap(caConfigMapName, namespaceFor(owner)).
		WithLabels(managedLabels(owner)).
		WithData(map[string]string{"ca.crt": pem})
}

const (
	caConfigMapName = "noma-agent-ca"
	caDir           = "/etc/noma/agent-ca"
)

// loginClaim is the user's Claude login, shared by all their workspaces:
// sign in once and every workspace starts signed in. Nothing deletes it
// with a workspace.
func loginClaim(owner string, cfg Config) *corev1.PersistentVolumeClaim {
	return claim(loginClaimName, namespaceFor(owner), managedLabels(owner), cfg.LoginStorageClass,
		cfg.LoginAccessMode, resource.MustParse(loginClaimSize))
}

// The login claim, and where workspaces mount it. Claude keeps its
// credentials in CLAUDE_SECURESTORAGE_CONFIG_DIR (checked in CI against the
// latest Claude, M3-9).
const (
	loginClaimName = "claude-login"
	loginClaimSize = "64Mi"
	loginDir       = "/var/lib/noma/claude-login"
)

// replicas is 1 for a running workspace and 0 for a stopped one, whose
// volume stays.
func replicas(ws *store.Workspace) int32 {
	if ws.Desired == store.DesiredStopped {
		return 0
	}
	return 1
}

// statefulSet runs the workspace pod: at most one, under gVisor, locked down.
func statefulSet(ws *store.Workspace, cfg Config) *appsv1ac.StatefulSetApplyConfiguration {
	env := []*corev1ac.EnvVarApplyConfiguration{
		corev1ac.EnvVar().WithName("NOMA_SERVER_URL").WithValue(cfg.ServerURL),
		corev1ac.EnvVar().WithName("NOMA_TOKEN_FILE").WithValue(tokenPath + "/token"),
		corev1ac.EnvVar().WithName("NOMA_WORKSPACE_DIR").WithValue(workspaceDir(ws)),
		corev1ac.EnvVar().WithName("NOMA_CA_FILE").WithValue(caDir + "/ca.crt"),
		corev1ac.EnvVar().WithName("HOME").WithValue(workspaceDir(ws) + "/home"),
		corev1ac.EnvVar().WithName("CLAUDE_SECURESTORAGE_CONFIG_DIR").WithValue(loginDir),
	}
	if cfg.ClaudeProfileURL != "" {
		env = append(env, corev1ac.EnvVar().WithName("NOMA_CLAUDE_PROFILE_URL").WithValue(cfg.ClaudeProfileURL))
	}
	image := cfg.Image
	var (
		command []string
		mounts  []*corev1ac.VolumeMountApplyConfiguration
		volumes []*corev1ac.VolumeApplyConfiguration
	)
	if ws.Image != "" {
		// The repo's own image, with our binaries copied in (M3-17).
		image = ws.Image
		command = []string{toolsDir + "/noma-agent"}
		env = append(env,
			corev1ac.EnvVar().WithName("NOMA_PROJECT_IMAGE").WithValue(ws.Image),
			corev1ac.EnvVar().WithName("NOMA_CLAUDE_BIN").WithValue(toolsDir+"/claude"))
		mounts = []*corev1ac.VolumeMountApplyConfiguration{
			corev1ac.VolumeMount().WithName("noma-tools").WithMountPath(toolsDir).WithReadOnly(true),
		}
		volumes = []*corev1ac.VolumeApplyConfiguration{
			corev1ac.Volume().WithName("noma-tools").WithEmptyDir(corev1ac.EmptyDirVolumeSource().
				WithMedium(corev1.StorageMediumMemory).WithSizeLimit(resource.MustParse(toolsSize))),
		}
	}
	return appsv1ac.StatefulSet(ws.ID, namespaceFor(ws.Owner)).
		WithLabels(workspaceLabels(ws)).
		WithSpec(appsv1ac.StatefulSetSpec().
			WithReplicas(replicas(ws)).
			WithServiceName(ws.ID).
			WithSelector(metav1ac.LabelSelector().WithMatchLabels(map[string]string{labelWorkspace: ws.ID})).
			WithTemplate(corev1ac.PodTemplateSpec().
				WithLabels(workspaceLabels(ws)).
				WithSpec(corev1ac.PodSpec().
					WithRuntimeClassName(cfg.RuntimeClass).
					WithServiceAccountName(ws.ID).
					WithAutomountServiceAccountToken(false).
					WithEnableServiceLinks(false).
					WithSecurityContext(corev1ac.PodSecurityContext().
						WithRunAsNonRoot(true).
						WithRunAsUser(workspaceUID).
						WithRunAsGroup(workspaceUID).
						WithFSGroup(workspaceUID).
						WithSeccompProfile(corev1ac.SeccompProfile().WithType(corev1.SeccompProfileTypeRuntimeDefault))).
					WithInitContainers(initContainers(ws, cfg)...).
					WithContainers(corev1ac.Container().
						WithName("workspace").
						WithImage(image).
						WithImagePullPolicy(corev1.PullIfNotPresent).
						WithCommand(command...).
						WithEnv(env...).
						WithResources(resources(cfg)).
						WithSecurityContext(corev1ac.SecurityContext().
							WithAllowPrivilegeEscalation(false).
							WithReadOnlyRootFilesystem(true).
							WithCapabilities(corev1ac.Capabilities().WithDrop("ALL"))).
						WithVolumeMounts(mounts...).
						WithVolumeMounts(
							corev1ac.VolumeMount().WithName("workspace").WithMountPath(workspaceDir(ws)),
							corev1ac.VolumeMount().WithName("agent-ca").WithMountPath(caDir).WithReadOnly(true),
							corev1ac.VolumeMount().WithName("login").WithMountPath(loginDir),
							corev1ac.VolumeMount().WithName("tmp").WithMountPath("/tmp"),
							corev1ac.VolumeMount().WithName("token").WithMountPath(tokenPath).WithReadOnly(true))).
					WithVolumes(volumes...).
					WithVolumes(
						corev1ac.Volume().WithName("workspace").
							WithPersistentVolumeClaim(corev1ac.PersistentVolumeClaimVolumeSource().WithClaimName(ws.ID)),
						corev1ac.Volume().WithName("agent-ca").
							WithConfigMap(corev1ac.ConfigMapVolumeSource().WithName(caConfigMapName)),
						corev1ac.Volume().WithName("login").
							WithPersistentVolumeClaim(corev1ac.PersistentVolumeClaimVolumeSource().WithClaimName(loginClaimName)),
						corev1ac.Volume().WithName("tmp").
							WithEmptyDir(corev1ac.EmptyDirVolumeSource()),
						// A token for noma-server only, bound to this pod and
						// rotated by the kubelet.
						corev1ac.Volume().WithName("token").
							WithProjected(corev1ac.ProjectedVolumeSource().WithSources(
								corev1ac.VolumeProjection().WithServiceAccountToken(
									corev1ac.ServiceAccountTokenProjection().
										WithAudience(api.TokenAudience).
										WithExpirationSeconds(tokenLifetime).
										WithPath("token"))))))))
}

// toolsDir holds noma-agent and claude in a pod running a project
// image; toolsSize bounds it, and counts against the pod's memory.
const (
	toolsDir  = "/opt/noma"
	toolsSize = "512Mi"
)

// initContainers copies our binaries out of the Noma workspace image into
// a project-image pod. A Noma-image pod needs none.
func initContainers(ws *store.Workspace, cfg Config) []*corev1ac.ContainerApplyConfiguration {
	if ws.Image == "" {
		return nil
	}
	// Files in the in-memory volume count against the container that
	// writes them, so the copy gets the workspace's own resources.
	return []*corev1ac.ContainerApplyConfiguration{corev1ac.Container().
		WithName("noma-tools").
		WithImage(cfg.Image).
		WithImagePullPolicy(corev1.PullIfNotPresent).
		WithCommand("cp", "/usr/local/bin/noma-agent", "/usr/local/bin/claude", toolsDir+"/").
		WithResources(resources(cfg)).
		WithSecurityContext(corev1ac.SecurityContext().
			WithAllowPrivilegeEscalation(false).
			WithReadOnlyRootFilesystem(true).
			WithCapabilities(corev1ac.Capabilities().WithDrop("ALL"))).
		WithVolumeMounts(corev1ac.VolumeMount().WithName("noma-tools").WithMountPath(toolsDir))}
}

func resources(cfg Config) *corev1ac.ResourceRequirementsApplyConfiguration {
	return corev1ac.ResourceRequirements().
		WithRequests(cfg.Resources.Requests).
		WithLimits(cfg.Resources.Limits)
}
