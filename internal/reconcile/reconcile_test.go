package reconcile

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestContainerProblem(t *testing.T) {
	waiting := func(reason string) corev1.ContainerState {
		return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}
	}
	terminated := func(reason string, code int32) corev1.ContainerState {
		return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: reason, ExitCode: code}}
	}
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	for _, tc := range []struct {
		name        string
		init        bool
		state, last corev1.ContainerState
		want        string
	}{
		{"running", false, running, corev1.ContainerState{}, ""},
		{"running after a restart", false, running, terminated("OOMKilled", 137), ""},
		{"being created", false, waiting("ContainerCreating"), corev1.ContainerState{}, ""},
		{"init containers running", false, waiting("PodInitializing"), corev1.ContainerState{}, ""},
		{"finished init container", true, terminated("Completed", 0), corev1.ContainerState{}, ""},
		{"image pull", false, waiting("ImagePullBackOff"), corev1.ContainerState{}, "ImagePullBackOff"},
		{"crash loop after OOM", false, waiting("CrashLoopBackOff"), terminated("OOMKilled", 137), "CrashLoopBackOff after OOMKilled"},
		{"just killed", false, terminated("OOMKilled", 137), corev1.ContainerState{}, "restarting after OOMKilled"},
		{"just exited", false, terminated("Error", 1), corev1.ContainerState{}, "restarting after Error"},
		{"failed init container", true, terminated("Error", 1), corev1.ContainerState{}, "restarting after Error"},
		{"main container exited cleanly", false, terminated("Completed", 0), corev1.ContainerState{}, "restarting after Completed"},
	} {
		cs := &corev1.ContainerStatus{State: tc.state, LastTerminationState: tc.last}
		if got := containerProblem(cs, tc.init); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
