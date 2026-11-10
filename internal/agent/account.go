package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MrDHat/noma/internal/api"
)

// DefaultProfileURL is Claude's profile endpoint, which names the account
// an access token belongs to (M3-26).
const DefaultProfileURL = "https://api.anthropic.com/api/oauth/profile"

// errAccountChanged means the login belongs to another Claude account than
// the one pinned for the user.
var errAccountChanged = errors.New("Claude account changed") //nolint:staticcheck // shown to the user as is

// accessToken returns Claude's access token and whether it is still fresh.
// Claude refreshes an expired one itself when it runs.
func accessToken(home string) (string, bool, error) {
	dir := os.Getenv("CLAUDE_SECURESTORAGE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(home, ".claude")
	}
	b, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if err != nil {
		return "", false, err
	}
	var creds struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(b, &creds); err != nil || creds.OAuth.AccessToken == "" {
		return "", false, errors.New("no access token in Claude's credentials")
	}
	fresh := time.UnixMilli(creds.OAuth.ExpiresAt).After(time.Now().Add(time.Minute))
	return creds.OAuth.AccessToken, fresh, nil
}

// claudeAccount asks the profile endpoint which account token belongs to.
// A token the endpoint refuses counts as another account: an inference-only
// token gets 403, and must not slip past the check.
func (a *Agent) claudeAccount(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.ProfileURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	client := &http.Client{Timeout: 15 * time.Second}
	if a.Client != nil && strings.HasPrefix(a.ProfileURL, a.Client.BaseURL+"/") {
		client = a.Client.HTTP // the fake profile on noma-server, in CI
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", fmt.Errorf("%w: the profile endpoint refused the token (%d)", errAccountChanged, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("profile endpoint: %s", resp.Status)
	}
	var p struct {
		Account struct {
			UUID string `json:"uuid"`
		} `json:"account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil || p.Account.UUID == "" {
		return "", errors.New("profile endpoint returned no account")
	}
	return p.Account.UUID, nil
}

// checkAccount checks the login's account against the user's pinned one.
// It returns errAccountChanged on a mismatch or a refused token. When the
// profile endpoint can't tell (an expired token, a network error) it lets
// Claude run; when noma-server can't answer, it keeps asking, so a broken
// check never quietly lets Claude through.
func (a *Agent) checkAccount(ctx context.Context, c *Claude) error {
	token, fresh, err := accessToken(c.Home)
	if err != nil {
		slog.Warn("check claude account", "err", err)
		return nil
	}
	if !fresh {
		slog.Info("claude's token has expired; checking its account once claude has refreshed it")
		return nil
	}
	id, err := a.claudeAccount(ctx, token)
	if errors.Is(err, errAccountChanged) {
		return err
	}
	if err != nil {
		slog.Warn("could not ask claude's profile endpoint", "err", err)
		return nil
	}
	changed, err := retry(ctx, func() (bool, error) {
		err := a.Client.CheckAccount(ctx, api.AgentAccount{Account: id})
		if errors.Is(err, errAccountChanged) {
			return true, nil
		}
		return false, err
	})
	switch {
	case err != nil:
		return err // ctx is done
	case changed:
		return errAccountChanged
	}
	return nil
}
