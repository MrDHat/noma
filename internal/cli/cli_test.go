package cli

import (
	"testing"

	"github.com/MrDHat/noma/internal/api"
)

func TestProgressShowsSilentAgent(t *testing.T) {
	for _, tc := range []struct {
		ws   api.Workspace
		want string
	}{
		{api.Workspace{Phase: api.PhaseReady}, "  ready"},
		{api.Workspace{Phase: api.PhaseCloning, Message: "https://x/y.git"}, "  cloning: https://x/y.git"},
		{api.Workspace{Phase: api.PhaseReady, AgentSilentSeconds: 330}, "  ready (agent silent for 5m)"},
	} {
		if got := progress(&tc.ws); got != tc.want {
			t.Errorf("progress = %q, want %q", got, tc.want)
		}
	}
}
