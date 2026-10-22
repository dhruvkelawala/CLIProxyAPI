package executor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v8/sdk/translator"
)

func TestClaudeQuotaRecoveryRequiresConfirmedAllowance(t *testing.T) {
	const available = `{"five_hour":{"utilization":0},"seven_day":{"utilization":0}}`
	cases := []struct {
		name           string
		body           string
		status         int
		token          string
		baseURL        string
		transportError bool
		wantRecovered  bool
		wantError      bool
		wantSkip       bool
	}{
		{name: "restored allowance", body: available, wantRecovered: true},
		{name: "partially used allowance", body: `{"five_hour":{"utilization":38},"seven_day":{"utilization":22}}`, wantRecovered: true},
		{name: "usage breakdown is informational", body: `{"five_hour":{"utilization":0},"seven_day":{"utilization":0},"seven_day_breakdown":[{"model":"opus","percent":0}]}`, wantRecovered: true},
		{name: "session exhausted", body: `{"five_hour":{"utilization":100},"seven_day":{"utilization":22}}`},
		{name: "weekly exhausted", body: `{"five_hour":{"utilization":0},"seven_day":{"utilization":100}}`},
		{name: "missing weekly window", body: `{"five_hour":{"utilization":0}}`},
		{name: "unknown utilization", body: `{"five_hour":{},"seven_day":{"utilization":0}}`},
		{name: "null window", body: `{"five_hour":null,"seven_day":{"utilization":0}}`},
		{name: "locked window", body: `{"five_hour":{"utilization":0,"locked_reason":"account_locked"},"seven_day":{"utilization":0}}`},
		{name: "negative utilization", body: `{"five_hour":{"utilization":-1},"seven_day":{"utilization":0}}`},
		{name: "scoped exhaustion", body: `{"five_hour":{"utilization":0},"seven_day":{"utilization":0},"seven_day_opus":{"utilization":100}}`},
		{name: "reported limit exhausted", body: `{"five_hour":{"utilization":0},"seven_day":{"utilization":0},"limits":[{"percent":100}]}`},
		{name: "grant confirms exhausted", body: `{"five_hour":{"utilization":0},"seven_day":{"utilization":0},"cedar_ember":{"at_limit":true}}`},
		{name: "invalid response", body: `not json`, wantError: true},
		{name: "unauthorized", status: http.StatusUnauthorized, body: available},
		{name: "rate limited", status: http.StatusTooManyRequests, body: available},
		{name: "server error", status: http.StatusInternalServerError, body: available},
		{name: "transport failure", transportError: true, wantError: true},
		{name: "API key", token: "sk-ant-api-key", wantSkip: true},
		{name: "custom upstream", baseURL: "https://example.invalid", wantSkip: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := tc.token
			if token == "" {
				token = "sk-ant-oat-test-recovery"
			}
			called := false
			transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				called = true
				if req.Method != http.MethodGet || req.URL.Host != "api.anthropic.com" || req.URL.Path != "/api/oauth/usage" {
					t.Errorf("unexpected quota request: %s %s", req.Method, req.URL)
				}
				if req.Header.Get("Authorization") != "Bearer "+token {
					t.Error("usage lookup did not use the selected OAuth credential")
				}
				if tc.transportError {
					return nil, errors.New("upstream unavailable")
				}
				status := tc.status
				if status == 0 {
					status = http.StatusOK
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			ctx := context.WithValue(t.Context(), "cliproxy.roundtripper", http.RoundTripper(transport))
			recovered, err := NewClaudeExecutor(&config.Config{}).QuotaRecovered(ctx, &coreauth.Auth{
				Provider: "claude", Metadata: map[string]any{"access_token": token}, Attributes: map[string]string{"base_url": tc.baseURL},
			})
			if recovered != tc.wantRecovered || (err != nil) != tc.wantError {
				t.Fatalf("recovered=%v error=%v, want recovered=%v error=%v", recovered, err, tc.wantRecovered, tc.wantError)
			}
			if called == tc.wantSkip {
				t.Fatalf("upstream called=%v, want skip=%v", called, tc.wantSkip)
			}
		})
	}
}

func TestClaudeManualQuotaResetResumesRouting(t *testing.T) {
	const model = "claude-opus-5-5"
	id := uuid.NewString()
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"five_hour":{"utilization":0},"seven_day":{"utilization":0},"cedar_ember":{"at_limit":false}}`
		if req.URL.Path == "/v1/messages" {
			body = `{"id":"msg_recovered","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`
		} else if req.Method != http.MethodGet || req.URL.String() != "https://api.anthropic.com/api/oauth/usage?cedar_ember=1&skip_spend=1" {
			t.Errorf("unexpected upstream request: %s %s", req.Method, req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	ctx := context.WithValue(t.Context(), "cliproxy.roundtripper", http.RoundTripper(transport))
	manager := coreauth.NewManager(nil, nil, nil)
	manager.RegisterExecutor(NewClaudeExecutor(&config.Config{}))
	_, errRegister := manager.Register(ctx, &coreauth.Auth{
		ID: id, Provider: "claude", Status: coreauth.StatusActive,
		Metadata: map[string]any{
			"access_token": "sk-ant-oat-test-recovery",
			"expired":      time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			"account_uuid": "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		},
	})
	if errRegister != nil {
		t.Fatal(errRegister)
	}
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(id, "claude", []*registry.ModelInfo{{ID: model}})
	t.Cleanup(func() { reg.UnregisterClient(id) })
	retry := 7 * 24 * time.Hour
	manager.MarkResult(ctx, coreauth.Result{
		AuthID: id, Provider: "claude", Model: model, CredentialScope: true, RetryAfter: &retry,
		Error: &coreauth.Error{HTTPStatus: http.StatusTooManyRequests, Message: "weekly allowance exhausted"},
	})
	before, _ := manager.GetByID(id)
	if !before.Unavailable || reg.GetModelCount(model) != 0 {
		t.Fatal("quota rejection did not block routing")
	}
	manager.StartAutoRefresh(ctx, time.Hour)
	t.Cleanup(manager.StopAutoRefresh)
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(time.Millisecond)
	defer poll.Stop()
	for {
		auth, _ := manager.GetByID(id)
		if !auth.Unavailable && !auth.Quota.Exceeded && reg.GetModelCount(model) == 1 {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("Anthropic reports restored allowance but the proxy still blocks the credential")
		case <-poll.C:
		}
	}
	_, errExecute := manager.Execute(ctx, []string{"claude"}, cliproxyexecutor.Request{
		Model: model, Payload: []byte(`{"model":"claude-opus-5-5","max_tokens":8,"messages":[{"role":"user","content":"hello"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FormatClaude})
	if errExecute != nil {
		t.Fatalf("routing after manual quota reset: %v", errExecute)
	}
}
