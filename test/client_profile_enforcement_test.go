package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	auth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	executor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const enforcementRef = "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
const enforcementKey = "synthetic-profile-key"

type enforcementFixture struct {
	routingFixture
	cfg *internalconfig.Config
	ctx context.Context
}

func newEnforcementFixture(t *testing.T) enforcementFixture {
	t.Helper()
	f := newRoutingFixture(t)
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(t.TempDir())
	f.manager.SetStore(store)
	for _, id := range []string{"fixture-a", "fixture-b"} {
		a, _ := f.manager.GetByID(id)
		a.Metadata = map[string]any{"type": "codex"}
		a.FileName = id + ".json"
		if id == "fixture-a" {
			a.Metadata[clientprofiles.AccountRefMetadataKey] = enforcementRef
		}
		if _, err := f.manager.Update(context.Background(), a); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &internalconfig.Config{}
	cfg.APIKeys = []string{enforcementKey, "synthetic-legacy-key"}
	cfg.WebsocketAuth = true
	cfg.ClientProfiles = []clientprofiles.Profile{{Ref: enforcementRef, Label: "synthetic", Revision: 1, Policies: map[string]clientprofiles.Policy{"codex": {Mode: "only", AccountRef: enforcementRef}, "claude": {Mode: "automatic"}}}}
	cfg.ClientProfileKeys = []clientprofiles.Key{{Ref: enforcementRef, Label: "synthetic", ProfileRef: enforcementRef, Revision: 1, Fingerprint: clientprofiles.Fingerprint(enforcementKey)}}
	f.manager.SetConfig(cfg)
	snapshot, err := clientprofiles.Binding(cfg.ClientProfiles, cfg.ClientProfileKeys, enforcementKey)
	if err != nil {
		t.Fatal(err)
	}
	return enforcementFixture{f, cfg, clientprofiles.WithBinding(context.Background(), snapshot)}
}
func (f enforcementFixture) call(ctx context.Context, kind, model string, providers []string, opts executor.Options) error {
	req := executor.Request{Model: model, Payload: []byte(`{"input":[]}`)}
	switch kind {
	case "stream":
		stream, err := f.manager.ExecuteStream(ctx, providers, req, opts)
		if err != nil {
			return err
		}
		for chunk := range stream.Chunks {
			if chunk.Err != nil {
				return chunk.Err
			}
		}
		return nil
	case "count":
		_, err := f.manager.ExecuteCount(ctx, providers, req, opts)
		return err
	default:
		_, err := f.manager.Execute(ctx, providers, req, opts)
		return err
	}
}
func TestClientProfileEnforcementRequests(t *testing.T) {
	for _, kind := range []string{"execute", "stream", "count"} {
		for _, model := range []string{"gpt-fixture-main", "gpt-fixture-helper", "fixture-alias"} {
			t.Run(kind+"/"+model, func(t *testing.T) {
				f := newEnforcementFixture(t)
				if err := f.call(f.ctx, kind, model, []string{"codex"}, executor.Options{}); err != nil {
					t.Fatal(err)
				}
				f.recorder.failAccountA(503)
				if err := f.call(f.ctx, kind, model, []string{"codex"}, executor.Options{}); err == nil {
					t.Fatal("failed A succeeded")
				}
				if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err != nil {
					t.Fatal(err)
				}
				if err := f.invoke(kind, model, "fixture-b"); err != nil {
					t.Fatal("enabled B canary", err)
				}
				t.Logf("strict %s %s A only; enabled B canary=%v", kind, model, f.recorder.snapshot())
			})
		}
	}
}
func TestClientProfileEnforcementFailures(t *testing.T) {
	for _, mode := range []string{"disabled", "cooldown", "expired", "deleted", "duplicate", "unsupported-duplicate", "provider", "executor", "pin", "mixed", "home", "duplex", "stale", "missing-owner", "unknown-plugin", "bootstrap", "unavailable"} {
		for _, kind := range []string{"execute", "stream", "count"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				f := newEnforcementFixture(t)
				ctx := f.ctx
				providers := []string{"codex"}
				opts := executor.Options{}
				a, _ := f.manager.GetByID("fixture-a")
				switch mode {
				case "disabled":
					a.Disabled = true
					_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), a)
				case "expired":
					a.Metadata["access_token"] = "synthetic-expired"
					a.Metadata["expired"] = "2000-01-01T00:00:00Z"
					_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), a)
				case "cooldown":
					a.ModelStates = map[string]*auth.ModelState{"gpt-fixture-main": {Unavailable: true, NextRetryAfter: time.Now().Add(time.Hour), Quota: auth.QuotaState{Exceeded: true, NextRecoverAt: time.Now().Add(time.Hour)}}}
					_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), a)
				case "unavailable":
					a.Unavailable = true
					_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), a)
				case "deleted":
					f.manager.Remove(context.Background(), a.ID)
				case "duplicate":
					b, _ := f.manager.GetByID("fixture-b")
					b.Metadata[clientprofiles.AccountRefMetadataKey] = enforcementRef
					b.Disabled = true
					_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), b)
				case "unsupported-duplicate":
					b, _ := f.manager.GetByID("fixture-b")
					b.Metadata[clientprofiles.AccountRefMetadataKey] = enforcementRef
					b.Provider = "unknown"
					_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), b)
				case "provider":
					a.Provider = "claude"
					_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), a)
				case "executor":
					a.Attributes["compat_name"] = "codex"
					_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), a)
				case "pin":
					opts.Metadata = map[string]any{executor.PinnedAuthMetadataKey: "fixture-b"}
				case "mixed":
					providers = []string{"codex", "claude"}
				case "home":
					cfg := f.cfg.CloneForRuntime()
					cfg.Home.Enabled = true
					f.manager.SetConfig(cfg)
				case "duplex":
					ctx = executor.WithWebsocketInput(ctx, make(chan executor.WebsocketInput))
				case "stale":
					cfg := f.cfg.CloneForRuntime()
					cfg.ClientProfileKeys = nil
					f.manager.SetConfig(cfg)
				case "missing-owner":
					f.manager.SetConfig(&internalconfig.Config{})
				case "unknown-plugin":
					f.manager.SetPluginScheduler(&routingPlugin{mode: "unknown"})
				case "bootstrap":
					if kind != "stream" {
						return
					}
					f.recorder.bootstrapA = true
				}
				err := f.call(ctx, kind, "gpt-fixture-main", providers, opts)
				if err == nil {
					t.Fatal("strict failure succeeded")
				}
				if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s/%s error=%v observations=%v", mode, kind, err, f.recorder.snapshot())
			})
		}
	}
}
func (f enforcementFixture) server(t *testing.T) *httptest.Server {
	t.Helper()
	gin.SetMode(gin.TestMode)
	f.cfg.CommercialMode = true
	f.cfg.RemoteManagement.DisableControlPanel = true
	f.cfg.AuthDir = t.TempDir()
	t.Cleanup(func() { sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey) })
	gateway := api.NewServer(f.cfg, f.manager, sdkaccess.NewManager(), filepath.Join(t.TempDir(), "config.yaml"))
	server := httptest.NewServer(gateway.Handler())
	t.Cleanup(server.Close)
	return server
}

func TestClientProfileEnforcementHTTP(t *testing.T) {
	f := newEnforcementFixture(t)
	server := f.server(t)
	post := func(key string) (int, string) {
		req, _ := http.NewRequest("POST", server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-fixture-helper","input":[]}`))
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if errClose := resp.Body.Close(); errClose != nil {
				t.Errorf("close synthetic response: %v", errClose)
			}
		}()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	status, body := post(enforcementKey)
	if status != 200 {
		t.Fatal(status, body)
	}
	t.Logf("HTTP helper -> %d %s observations=%v", status, body, f.recorder.snapshot())
	a, _ := f.manager.GetByID("fixture-a")
	a.Disabled = true
	_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), a)
	status, body = post(enforcementKey)
	if status != 503 || !strings.Contains(body, `"code":"target_unavailable"`) {
		t.Fatal(status, body)
	}
	if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err != nil {
		t.Fatal(err)
	}
	t.Logf("HTTP disabled A -> %d %s observations=%v", status, body, f.recorder.snapshot())
	status, body = post("synthetic-legacy-key")
	if status != 200 {
		t.Fatal(status, body)
	}
	t.Logf("HTTP legacy -> %d %s observations=%v", status, body, f.recorder.snapshot())
}
func TestClientProfileEnforcementWebsocket(t *testing.T) {
	for _, mode := range []string{"reconnect", "key-delete", "profile-edit", "automatic-delete", "silent-rotation"} {
		t.Run(mode, func(t *testing.T) {
			f := newEnforcementFixture(t)
			if mode == "automatic-delete" {
				f.cfg.ClientProfiles[0].Policies["codex"] = clientprofiles.Policy{Mode: "automatic"}
				f.manager.SetConfig(f.cfg)
			}
			server := f.server(t)
			url := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/responses"
			dial := func() *websocket.Conn {
				conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": []string{"Bearer " + enforcementKey}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { conn.Close() })
				return conn
			}
			send := func(conn *websocket.Conn, body string) string {
				if err := conn.WriteMessage(websocket.TextMessage, []byte(body)); err != nil {
					t.Fatal(err)
				}
				_, payload, err := conn.ReadMessage()
				if err != nil {
					return fmt.Sprint(err)
				}
				var event map[string]any
				if err := json.Unmarshal(payload, &event); err != nil {
					t.Fatal(err)
				}
				t.Logf("WS reply=%s observations=%v", payload, f.recorder.snapshot())
				return string(payload)
			}
			conn := dial()
			body := `{"type":"response.create","model":"gpt-fixture-main","input":[]}`
			if reply := send(conn, body); !strings.Contains(reply, "response.completed") {
				t.Fatal(reply)
			}
			if mode == "reconnect" {
				f.recorder.failAccountA(401)
				send(conn, `{"type":"response.create","previous_response_id":"fixture-response","input":[]}`)
				send(dial(), body)
			} else {
				cfg := f.cfg.CloneForRuntime()
				if mode == "profile-edit" {
					cfg.ClientProfiles[0].Revision++
				} else {
					cfg.ClientProfileKeys = nil
				}
				f.manager.SetConfig(cfg)
				if reply := send(conn, body); !strings.Contains(reply, "client_profile_stale") {
					t.Fatal(reply)
				}
			}
			if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type providerRoutingRecorder struct {
	*routingRecorder
	provider string
}

func (r *providerRoutingRecorder) Identifier() string { return r.provider }
func TestClientProfileEnforcementIndependentProviders(t *testing.T) {
	f := newEnforcementFixture(t)
	f.manager.RegisterExecutor(&providerRoutingRecorder{f.recorder, "claude"})
	for _, id := range []string{"fixture-a", "fixture-b"} {
		a, _ := f.manager.GetByID(id)
		a.Provider = "claude"
		a.Metadata["type"] = "claude"
		if _, err := f.manager.Update(context.Background(), a); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(id, "claude", []*registry.ModelInfo{{ID: "claude-fixture-main"}, {ID: "claude-fixture-helper"}})
		f.manager.RefreshSchedulerEntry(id)
	}
	cfg := f.cfg.CloneForRuntime()
	cfg.ClientProfiles[0].Policies = map[string]clientprofiles.Policy{"claude": {Mode: "only", AccountRef: enforcementRef}, "codex": {Mode: "automatic"}}
	f.manager.SetConfig(cfg)
	snapshot, err := clientprofiles.Binding(cfg.ClientProfiles, cfg.ClientProfileKeys, enforcementKey)
	if err != nil {
		t.Fatal(err)
	}
	ctx := clientprofiles.WithBinding(context.Background(), snapshot)
	for _, kind := range []string{"execute", "stream", "count"} {
		if err := f.call(ctx, kind, "claude-fixture-helper", []string{"claude"}, executor.Options{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err != nil {
		t.Fatal(err)
	}
	_, err = f.manager.Register(context.Background(), &auth.Auth{ID: "fixture-c", Provider: "codex", Status: auth.StatusActive, Metadata: map[string]any{"type": "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	registry.GetGlobalRegistry().RegisterClient("fixture-c", "codex", []*registry.ModelInfo{{ID: "gpt-fixture-main"}})
	t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient("fixture-c") })
	f.manager.RefreshSchedulerEntry("fixture-c")
	if err := f.call(ctx, "execute", "gpt-fixture-main", []string{"codex"}, executor.Options{}); err != nil {
		t.Fatal(err)
	}
	got := f.recorder.snapshot()
	if got[len(got)-1].Account != "fixture-c" {
		t.Fatal(got)
	}
	t.Logf("ClaudeOnly stayed on A, independent CodexAutomatic used C: %v", got)
}

type changingProfileRecorder struct {
	*routingRecorder
	manager *auth.Manager
	mode    string
}

func (*changingProfileRecorder) ShouldPrepareRequestAuth(*auth.Auth) bool { return true }
func (r *changingProfileRecorder) PrepareRequestAuth(ctx context.Context, a *auth.Auth) (*auth.Auth, error) {
	switch r.mode {
	case "removed":
		r.manager.Remove(auth.WithSkipPersist(context.Background()), a.ID)
	case "duplicate":
		b, _ := r.manager.GetByID("fixture-b")
		b.Metadata[clientprofiles.AccountRefMetadataKey] = enforcementRef
		_, _ = r.manager.Update(auth.WithSkipPersist(context.Background()), b)
	default:
		changed := a.Clone()
		changed.Provider = "claude"
		_, _ = r.manager.Update(auth.WithSkipPersist(context.Background()), changed)
	}
	return nil, nil
}

func TestClientProfileEnforcementFinalValidation(t *testing.T) {
	for _, kind := range []string{"execute", "stream", "count"} {
		for _, mode := range []string{"provider", "removed", "duplicate"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				f := newEnforcementFixture(t)
				f.manager.RegisterExecutor(&changingProfileRecorder{f.recorder, f.manager, mode})
				if err := f.call(f.ctx, kind, "gpt-fixture-main", []string{"codex"}, executor.Options{}); err == nil {
					t.Fatal("changed target executed")
				}
				if got := f.recorder.snapshot(); len(got) != 0 {
					t.Fatal("upstream execution occurred", got)
				}
			})
		}
	}
}

func TestClientProfileEnforcementDirectRoutes(t *testing.T) {
	f := newEnforcementFixture(t)
	server := f.server(t)
	for _, path := range []string{"/v1/realtime/client_secrets", "/v1/realtime/sessions", "/v1/realtime", "/v1/live", "/v1/alpha/search", "/backend-api/codex/alpha/search", "/v1/videos", "/openai/v1/videos"} {
		req, _ := http.NewRequest("POST", server.URL+path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+enforcementKey)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 503 || !strings.Contains(string(body), "profile_protocol_unsupported") {
			t.Fatalf("%s status=%d body=%s", path, resp.StatusCode, body)
		}
		t.Logf("HTTP %s -> %d %s; no upstream events", path, resp.StatusCode, body)
	}
	if got := f.recorder.snapshot(); len(got) != 0 {
		t.Fatal(got)
	}
}

type refreshProfileRecorder struct {
	*routingRecorder
	refreshed      bool
	changeProvider bool
}

func (r *refreshProfileRecorder) Refresh(_ context.Context, a *auth.Auth) (*auth.Auth, error) {
	r.refreshed = true
	updated := a.Clone()
	updated.Metadata["access_token"] = "synthetic-refreshed"
	if r.changeProvider {
		updated.Provider = "claude"
	}
	r.routingRecorder.mu.Lock()
	r.routingRecorder.failA = false
	r.routingRecorder.mu.Unlock()
	return updated, nil
}
func TestClientProfileEnforcementRefresh(t *testing.T) {
	for _, kind := range []string{"execute", "stream", "count"} {
		for _, changed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/change-provider=%t", kind, changed), func(t *testing.T) {
				f := newEnforcementFixture(t)
				a, _ := f.manager.GetByID("fixture-a")
				a.Metadata["access_token"] = "synthetic-expired"
				a.Metadata["refresh_token"] = "synthetic-refresh"
				a.Metadata["expired"] = "2030-01-01T00:00:00Z"
				_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), a)
				r := &refreshProfileRecorder{routingRecorder: f.recorder, changeProvider: changed}
				f.manager.RegisterExecutor(r)
				f.recorder.failAccountA(401)
				err := f.call(f.ctx, kind, "gpt-fixture-main", []string{"codex"}, executor.Options{})
				if !r.refreshed {
					t.Fatal("expired/unauthorized credential was not refreshed", err)
				}
				if changed && err == nil {
					t.Fatal("refreshed provider escaped constraint")
				}
				if !changed && err != nil {
					t.Fatal(err)
				}
				if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err != nil {
					t.Fatal(err)
				}
				if got := f.recorder.snapshot(); len(got) != map[bool]int{true: 1, false: 2}[changed] {
					t.Fatal(got)
				}
				t.Logf("refresh changed=%t error=%v observations=%v", changed, err, f.recorder.snapshot())
			})
		}
	}
}

type escapingProfileScheduler struct{}

func (*escapingProfileScheduler) PickAuth(_ context.Context, q pluginapi.SchedulerPickRequest) (pluginapi.SchedulerPickResponse, bool, error) {
	for _, candidate := range q.Candidates {
		if candidate.ID != "fixture-a" {
			return pluginapi.SchedulerPickResponse{}, true, fmt.Errorf("candidate pool escaped to %s", candidate.ID)
		}
	}
	return pluginapi.SchedulerPickResponse{Handled: true, AuthID: "fixture-b"}, true, nil
}
func TestClientProfileEnforcementPluginPool(t *testing.T) {
	for _, kind := range []string{"execute", "stream", "count"} {
		t.Run(kind, func(t *testing.T) {
			f := newEnforcementFixture(t)
			f.manager.SetPluginScheduler(&escapingProfileScheduler{})
			err := f.call(f.ctx, kind, "gpt-fixture-main", []string{"codex"}, executor.Options{})
			if err == nil || !strings.Contains(err.Error(), "profile_scheduler_invalid") {
				t.Fatal(err)
			}
			if got := f.recorder.snapshot(); len(got) != 0 {
				t.Fatal(got)
			}
		})
	}
}

func TestClientProfileEnforcementImages(t *testing.T) {
	f := newEnforcementFixture(t)
	for _, id := range []string{"fixture-a", "fixture-b"} {
		registry.GetGlobalRegistry().RegisterClient(id, "codex", []*registry.ModelInfo{{ID: "gpt-5.4-mini"}, {ID: "gpt-image-2"}})
		f.manager.RefreshSchedulerEntry(id)
	}
	server := f.server(t)
	for _, disabled := range []bool{false, true} {
		if disabled {
			a, _ := f.manager.GetByID("fixture-a")
			a.Disabled = true
			_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), a)
		}
		req, _ := http.NewRequest("POST", server.URL+"/v1/images/generations", strings.NewReader(`{"model":"gpt-image-2","prompt":"synthetic"}`))
		req.Header.Set("Authorization", "Bearer "+enforcementKey)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !disabled && resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode, string(body))
		}
		if disabled && (resp.StatusCode != 503 || !strings.Contains(string(body), "target_unavailable")) {
			t.Fatal(resp.StatusCode, string(body))
		}
		if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err != nil {
			t.Fatal(err)
		}
		t.Logf("HTTP images disabled=%t -> %d %s observations=%v", disabled, resp.StatusCode, body, f.recorder.snapshot())
	}
}

type enforcementFrontend struct{}

func (*enforcementFrontend) Identifier() string { return "synthetic-profile-frontend" }
func (*enforcementFrontend) Authenticate(context.Context, *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	return &sdkaccess.Result{Provider: "synthetic-profile-frontend", Principal: "opaque-plugin-user"}, nil
}
func TestClientProfileEnforcementExclusiveFrontendHTTP(t *testing.T) {
	sdkaccess.RegisterProvider("synthetic-profile-frontend", &enforcementFrontend{})
	sdkaccess.SetExclusiveProvider("synthetic-profile-frontend")
	t.Cleanup(func() { sdkaccess.ClearExclusiveProvider(); sdkaccess.UnregisterProvider("synthetic-profile-frontend") })
	f := newEnforcementFixture(t)
	server := f.server(t)
	for _, disabled := range []bool{false, true} {
		if disabled {
			a, _ := f.manager.GetByID("fixture-a")
			a.Disabled = true
			_, _ = f.manager.Update(auth.WithSkipPersist(context.Background()), a)
		}
		req, _ := http.NewRequest("POST", server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-fixture-helper","input":[]}`))
		req.Header.Set("Authorization", "Bearer "+enforcementKey)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if (!disabled && resp.StatusCode != 200) || (disabled && (resp.StatusCode != 503 || !strings.Contains(string(body), "target_unavailable"))) {
			t.Fatal(resp.StatusCode, string(body))
		}
		if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err != nil {
			t.Fatal(err)
		}
		t.Logf("HTTP exclusive frontend disabled=%t -> %d %s observations=%v", disabled, resp.StatusCode, body, f.recorder.snapshot())
	}
}

func TestClientProfileEnforcementAutomaticPool(t *testing.T) {
	f := newEnforcementFixture(t)
	cfg := f.cfg.CloneForRuntime()
	cfg.ClientProfiles[0].Policies["codex"] = clientprofiles.Policy{Mode: "automatic"}
	f.manager.SetConfig(cfg)
	snapshot, err := clientprofiles.Binding(cfg.ClientProfiles, cfg.ClientProfileKeys, enforcementKey)
	if err != nil {
		t.Fatal(err)
	}
	ctx := clientprofiles.WithBinding(context.Background(), snapshot)
	for range 2 {
		if err := f.call(ctx, "execute", "gpt-fixture-main", []string{"codex"}, executor.Options{}); err != nil {
			t.Fatal(err)
		}
	}
	got := f.recorder.snapshot()
	if len(got) != 2 || got[0].Account == got[1].Account {
		t.Fatal("Automatic did not retain eligible pool", got)
	}
	t.Logf("Automatic profile eligible pool observations=%v", got)
}

func TestClientProfileEnforcementSteeringRejectsBeforeUpgrade(t *testing.T) {
	for _, mode := range []string{"only", "automatic"} {
		t.Run(mode, func(t *testing.T) {
			f := newEnforcementFixture(t)
			f.cfg.Codex.ResponseSteering = true
			if mode == "automatic" {
				f.cfg.ClientProfiles[0].Policies["codex"] = clientprofiles.Policy{Mode: "automatic"}
			}
			server := f.server(t)
			url := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/responses"
			conn, resp, err := websocket.DefaultDialer.Dial(url, http.Header{"Authorization": []string{"Bearer " + enforcementKey}})
			if conn != nil {
				conn.Close()
			}
			if resp != nil {
				defer func() {
					if errClose := resp.Body.Close(); errClose != nil {
						t.Errorf("close synthetic response: %v", errClose)
					}
				}()
			}
			if err == nil || resp == nil || resp.StatusCode != 503 {
				t.Fatal("bound steering was not rejected before upgrade", err, resp)
			}
			if got := f.recorder.snapshot(); len(got) != 0 {
				t.Fatal(got)
			}
		})
	}
}

func TestClientProfileEnforcementBoundWSOwnerLoss(t *testing.T) {
	for _, mode := range []string{"only", "automatic"} {
		t.Run(mode, func(t *testing.T) { testClientProfileEnforcementBoundWSOwnerLoss(t, mode) })
	}
}
func testClientProfileEnforcementBoundWSOwnerLoss(t *testing.T, mode string) {
	f := newEnforcementFixture(t)
	if mode == "automatic" {
		f.cfg.ClientProfiles[0].Policies["codex"] = clientprofiles.Policy{Mode: "automatic"}
	}
	f.cfg.CommercialMode = true
	f.cfg.RemoteManagement.DisableControlPanel = true
	f.cfg.AuthDir = t.TempDir()
	access := sdkaccess.NewManager()
	gateway := api.NewServer(f.cfg, f.manager, access, filepath.Join(t.TempDir(), "config.yaml"))
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()
	t.Cleanup(func() { sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey) })
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", http.Header{"Authorization": []string{"Bearer " + enforcementKey}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if errClose := conn.Close(); errClose != nil {
			t.Errorf("close synthetic websocket: %v", errClose)
		}
	}()
	turn := func() string {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-fixture-main","input":[]}`)); err != nil {
			t.Fatal(err)
		}
		_, b, err := conn.ReadMessage()
		if err != nil {
			return err.Error()
		}
		return string(b)
	}
	if b := turn(); !strings.Contains(b, "response.completed") {
		t.Fatal(b)
	}
	access.SetProvidersAndCredentialOwner(access.Providers(), nil)
	b := turn()
	if calls := f.recorder.snapshot(); len(calls) != 1 {
		t.Fatalf("owner loss performed upstream effects: %v", calls)
	}
	t.Logf("turn after configured credential owner removed: %s; calls=%v", b, f.recorder.snapshot())
	if !strings.Contains(b, "profile_owner_unavailable") {
		t.Fatalf("bound socket continued after owner loss: %s", b)
	}
}

type profileInjectRecorder struct {
	*routingRecorder
	injected string
}

func (r *profileInjectRecorder) PrepareRequest(req *http.Request, a *auth.Auth) error {
	r.injected = a.ID
	req.Header.Set("Authorization", "Bearer synthetic-"+a.ID)
	return nil
}
func TestClientProfileEnforcementStrictInjectCredentials(t *testing.T) {
	f := newEnforcementFixture(t)
	r := &profileInjectRecorder{routingRecorder: f.recorder}
	f.manager.RegisterExecutor(r)
	req, _ := http.NewRequestWithContext(f.ctx, "POST", "http://synthetic.invalid/", nil)
	err := f.manager.InjectCredentials(req, "fixture-b")
	t.Logf("strict A-only direct injection error=%v injected=%s", err, r.injected)
	if err == nil || !strings.Contains(err.Error(), "profile_protocol_unsupported") || r.injected != "" || req.Header.Get("Authorization") != "" {
		t.Fatalf("strict direct injection performed effects on %s: %v", r.injected, err)
	}
	legacyReq, errRequest := http.NewRequest("POST", "http://synthetic.invalid/", nil)
	if errRequest != nil {
		t.Fatal(errRequest)
	}
	if errLegacy := f.manager.InjectCredentials(legacyReq, "fixture-b"); errLegacy != nil || r.injected != "fixture-b" {
		t.Fatalf("legacy injection changed: injected=%s err=%v", r.injected, errLegacy)
	}
}
