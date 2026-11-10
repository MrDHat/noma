package server

import (
	"testing"
	"time"

	"github.com/MrDHat/noma/internal/api"
	"github.com/MrDHat/noma/internal/store"
)

func TestAgentSilence(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name  string
		phase string
		seen  time.Time
		want  time.Duration
	}{
		{"healthy agent", api.PhaseReady, now.Add(-90 * time.Second), 0},
		{"silent agent", api.PhaseReady, now.Add(-5 * time.Minute), 5 * time.Minute},
		{"silent while signing in", api.PhaseSignIn, now.Add(-3 * time.Minute), 3 * time.Minute},
		{"never polled", api.PhaseReady, time.Time{}, 0},
		{"pod not running", api.PhaseStarting, now.Add(-time.Hour), 0},
		{"stopped", api.PhaseStopped, now.Add(-time.Hour), 0},
	} {
		ws := &store.Workspace{Phase: tc.phase, AgentSeenAt: tc.seen}
		if got := agentSilence(ws, now); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
