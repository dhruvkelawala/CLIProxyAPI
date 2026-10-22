package executor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/runtime/executor/helps"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// QuotaRecovered checks included Claude OAuth allowance without redeeming resets or generating tokens.
func (e *ClaudeExecutor) QuotaRecovered(ctx context.Context, auth *coreauth.Auth) (bool, error) {
	token, baseURL := claudeCreds(auth)
	if auth == nil || !strings.EqualFold(auth.Provider, "claude") || !isClaudeOAuthToken(token) ||
		(strings.TrimRight(baseURL, "/") != "" && strings.TrimRight(baseURL, "/") != "https://api.anthropic.com") {
		return false, nil
	}
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.anthropic.com/api/oauth/usage?cedar_ember=1&skip_spend=1", nil)
	if errRequest != nil {
		return false, errRequest
	}
	helps.ApplyClaudeDefaultDeviceProfileHeaders(req, e.cfg)
	req.Header.Set("Anthropic-Beta", "oauth-2025-04-20")
	resp, errHTTP := e.HttpRequest(ctx, auth, req)
	if errHTTP != nil {
		return false, errHTTP
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.WithError(errClose).Debug("close Claude usage response")
		}
	}()
	if resp.StatusCode != http.StatusOK {
		var retryAfter *time.Duration
		now := time.Now()
		if deadline, ok := helps.ParseRetryAfterHeader(resp.Header.Get("Retry-After"), now); ok && deadline.After(now) {
			delay := deadline.Sub(now)
			retryAfter = &delay
		}
		return false, statusErr{code: resp.StatusCode, msg: "Claude usage lookup failed", retryAfter: retryAfter}
	}
	body, errRead := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if errRead != nil {
		return false, errRead
	}
	return helps.ClaudeUsageAvailable(body)
}
