package test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	auth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/tidwall/gjson"
)

func TestClientProfileEnforcementClaudeErrorEnvelope(t *testing.T) {
	f := newEnforcementFixture(t)
	f.manager.RegisterExecutor(&providerRoutingRecorder{f.recorder, "claude"})
	for _, id := range []string{"fixture-a", "fixture-b"} {
		a, _ := f.manager.GetByID(id)
		a.Provider = "claude"
		a.Metadata["type"] = "claude"
		a.Disabled = id == "fixture-a"
		if _, err := f.manager.Update(auth.WithSkipPersist(context.Background()), a); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(id, "claude", []*registry.ModelInfo{{ID: "claude-fixture-helper"}})
		f.manager.RefreshSchedulerEntry(id)
	}
	f.cfg.ClientProfiles[0].Policies = map[string]clientprofiles.Policy{
		"claude": {Mode: "only", AccountRef: enforcementRef}, "codex": {Mode: "automatic"},
	}
	f.manager.SetConfig(f.cfg)
	server := f.server(t)
	for _, scenario := range []struct{ path, payload string }{
		{"/v1/messages", `{"model":"claude-fixture-helper","messages":[]}`},
		{"/v1/messages/count_tokens", `{"model":"claude-fixture-helper","messages":[]}`},
		{"/v1/messages", `{"model":"claude-fixture-helper","messages":[],"stream":true}`},
	} {
		req, _ := http.NewRequest(http.MethodPost, server.URL+scenario.path, strings.NewReader(scenario.payload))
		req.Header.Set("Authorization", "Bearer "+enforcementKey)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, errRead := io.ReadAll(resp.Body)
		errClose := resp.Body.Close()
		if errRead != nil || errClose != nil {
			t.Fatalf("read/close synthetic response: %v/%v", errRead, errClose)
		}
		t.Logf("POST %s stream=%t -> %d %s upstream_attempts=%d", scenario.path, strings.Contains(scenario.payload, "stream"), resp.StatusCode, body, len(f.recorder.snapshot()))
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", resp.StatusCode)
		}
		for path, want := range map[string]string{
			"type": "error", "error.type": "api_error", "error.message": "target_unavailable",
			"error.code": "target_unavailable", "error.field": "claude",
		} {
			if got := gjson.GetBytes(body, path).String(); got != want {
				t.Errorf("%s = %q, want %q", path, got, want)
			}
		}
	}
	if got := f.recorder.snapshot(); len(got) != 0 {
		t.Fatalf("unavailable strict target reached upstream: %v", got)
	}
}
