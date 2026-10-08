package test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	internalconfig "github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/api/handlers/openai"
	auth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executionregistry"
	executor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type routingObservation struct{ Account, Kind string }
type routingRecorder struct {
	mu            sync.Mutex
	observations  []routingObservation
	failA         bool
	failureStatus int
	bootstrapA    bool
}

func (*routingRecorder) Identifier() string { return "codex" }
func (r *routingRecorder) record(a *auth.Auth, kind string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.observations = append(r.observations, routingObservation{a.ID, kind})
	if r.failA && a.ID == "fixture-a" {
		return &auth.Error{Code: "unavailable", Message: "fixture unavailable", HTTPStatus: r.failureStatus, Retryable: true}
	}
	return nil
}
func (r *routingRecorder) failAccountA(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failA = true
	r.failureStatus = status
}
func (r *routingRecorder) snapshot() []routingObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]routingObservation(nil), r.observations...)
}
func (r *routingRecorder) Execute(_ context.Context, a *auth.Auth, _ executor.Request, _ executor.Options) (executor.Response, error) {
	return executor.Response{Payload: []byte(`{}`)}, r.record(a, "execute")
}
func (r *routingRecorder) CountTokens(_ context.Context, a *auth.Auth, _ executor.Request, _ executor.Options) (executor.Response, error) {
	return executor.Response{Payload: []byte(`{"tokens":1}`)}, r.record(a, "count")
}
func (r *routingRecorder) ExecuteStream(_ context.Context, a *auth.Auth, _ executor.Request, _ executor.Options) (*executor.StreamResult, error) {
	if err := r.record(a, "stream"); err != nil {
		return nil, err
	}
	ch := make(chan executor.StreamChunk, 1)
	if r.bootstrapA && a.ID == "fixture-a" {
		ch <- executor.StreamChunk{Err: &auth.Error{Code: "unauthorized", Message: "fixture bootstrap failure", HTTPStatus: 401}}
		close(ch)
		return &executor.StreamResult{Chunks: ch}, nil
	}
	ch <- executor.StreamChunk{Payload: []byte(`{"type":"response.completed","response":{"id":"fixture-response","output":[]}}`)}
	close(ch)
	return &executor.StreamResult{Chunks: ch}, nil
}
func (*routingRecorder) Refresh(_ context.Context, a *auth.Auth) (*auth.Auth, error) { return a, nil }
func (*routingRecorder) HttpRequest(context.Context, *auth.Auth, *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unsupported fixture transport")
}

type routingPlugin struct {
	mode  string
	calls int
}

func (p *routingPlugin) HasScheduler() bool { return p.mode != "inactive" }
func (p *routingPlugin) PickAuth(_ context.Context, q pluginapi.SchedulerPickRequest) (pluginapi.SchedulerPickResponse, bool, error) {
	p.calls++
	if p.mode == "unhandled" {
		return pluginapi.SchedulerPickResponse{}, false, nil
	}
	if p.mode == "unknown" {
		return pluginapi.SchedulerPickResponse{Handled: true, AuthID: "missing"}, true, nil
	}
	for _, a := range q.Candidates {
		if a.ID == "fixture-a" {
			return pluginapi.SchedulerPickResponse{Handled: true, AuthID: a.ID}, true, nil
		}
	}
	return pluginapi.SchedulerPickResponse{Handled: true, Reject: true, RejectReason: "fixture A unavailable"}, true, nil
}

type routingFixture struct {
	manager  *auth.Manager
	recorder *routingRecorder
}

func newRoutingFixture(t *testing.T) routingFixture {
	t.Helper()
	r := &routingRecorder{}
	m := auth.NewManager(nil, &auth.RoundRobinSelector{}, nil)
	m.RegisterExecutor(r)
	m.SetRetryConfig(0, 0, 0)
	models := []*registry.ModelInfo{{ID: "gpt-fixture-main"}, {ID: "gpt-fixture-helper"}, {ID: "fixture-alias"}}
	for _, id := range []string{"fixture-a", "fixture-b"} {
		if _, err := m.Register(context.Background(), &auth.Auth{ID: id, Provider: "codex", Status: auth.StatusActive, Attributes: map[string]string{"websockets": "true"}}); err != nil {
			t.Fatal(err)
		}
		registry.GetGlobalRegistry().RegisterClient(id, "codex", models)
		t.Cleanup(func() { registry.GetGlobalRegistry().UnregisterClient(id) })
		m.RefreshSchedulerEntry(id)
	}
	m.SetOAuthModelAlias(map[string][]internalconfig.OAuthModelAlias{"codex": {{Name: "gpt-fixture-main", Alias: "fixture-alias", Fork: true}}})
	return routingFixture{m, r}
}
func (f routingFixture) invoke(kind, model, pin string) error {
	opts := executor.Options{Metadata: map[string]any{executor.CallerScopeMetadataKey: "fixture-caller"}}
	if pin != "" {
		opts.Metadata[executor.PinnedAuthMetadataKey] = pin
	}
	req := executor.Request{Model: model, Payload: []byte(`{"input":[]}`)}
	switch kind {
	case "stream":
		s, err := f.manager.ExecuteStream(context.Background(), []string{"codex"}, req, opts)
		if err != nil {
			return err
		}
		for c := range s.Chunks {
			if c.Err != nil {
				return c.Err
			}
		}
		return nil
	case "count":
		_, err := f.manager.ExecuteCount(context.Background(), []string{"codex"}, req, opts)
		return err
	default:
		_, err := f.manager.Execute(context.Background(), []string{"codex"}, req, opts)
		return err
	}
}
func onlyAccount(observations []routingObservation, want string) error {
	for _, o := range observations {
		if o.Account != want {
			return fmt.Errorf("account %s served %s, wanted %s", o.Account, o.Kind, want)
		}
	}
	return nil
}

func TestClientProfileFixtureLibrary(t *testing.T) {
	for _, kind := range []string{"execute", "stream", "count"} {
		for _, model := range []string{"gpt-fixture-main", "gpt-fixture-helper", "fixture-alias"} {
			t.Run(kind+"/"+model, func(t *testing.T) {
				f := newRoutingFixture(t)
				if err := f.invoke(kind, model, "fixture-a"); err != nil {
					t.Fatal(err)
				}
				if got := f.recorder.snapshot(); len(got) != 1 || got[0].Account != "fixture-a" {
					t.Fatalf("first observations=%v", got)
				}
				f.recorder.failAccountA(http.StatusServiceUnavailable)
				if err := f.invoke(kind, model, "fixture-a"); err == nil {
					t.Fatal("unavailable A succeeded")
				}
				if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err != nil {
					t.Fatal(err)
				}
				if err := f.invoke(kind, model, "fixture-b"); err != nil {
					t.Fatalf("B must remain enabled: %v", err)
				}
				if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err == nil {
					t.Fatal("canary failed to detect B")
				}
				t.Logf("observed %v; strict pin failed closed; B canary detected", f.recorder.snapshot())
			})
		}
	}
}
func TestClientProfileFixturePlugin(t *testing.T) {
	for _, kind := range []string{"execute", "stream", "count"} {
		for _, model := range []string{"gpt-fixture-main", "gpt-fixture-helper", "fixture-alias"} {
			for _, mode := range []string{"strict", "unhandled", "unknown", "missing", "inactive"} {
				t.Run(kind+"/"+model+"/"+mode, func(t *testing.T) {
					f := newRoutingFixture(t)
					p := &routingPlugin{mode: mode}
					if mode != "missing" {
						f.manager.SetPluginScheduler(p)
					}
					if err := f.invoke(kind, model, ""); err != nil {
						t.Fatal(err)
					}
					f.recorder.failAccountA(http.StatusServiceUnavailable)
					err := f.invoke(kind, model, "")
					got := f.recorder.snapshot()
					leaked := onlyAccount(got, "fixture-a") != nil
					if mode == "strict" {
						if err == nil || leaked {
							t.Fatalf("strict plugin err=%v observations=%v", err, got)
						}
					} else if !leaked && !(mode == "inactive" && model == "fixture-alias" && err != nil) {
						t.Fatalf("expected fallback to B, err=%v observations=%v", err, got)
					}
					t.Logf("mode=%s plugin_calls=%d observations=%v error=%v", mode, p.calls, got, err)
				})
			}
		}
	}
}
func TestClientProfileFixtureWebsocketReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newRoutingFixture(t)
	h := openai.NewOpenAIResponsesAPIHandler(handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, f.manager))
	router := gin.New()
	router.GET("/v1/responses", h.ResponsesWebsocket)
	server := httptest.NewServer(router)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/responses"
	send := func(conn *websocket.Conn, body string) error {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(body)); err != nil {
			return err
		}
		_, payload, err := conn.ReadMessage()
		var event struct {
			Type string `json:"type"`
		}
		if err == nil {
			if errDecode := json.Unmarshal(payload, &event); errDecode != nil {
				return fmt.Errorf("decode fixture websocket event: %w", errDecode)
			}
			if event.Type != "response.completed" {
				return fmt.Errorf("unexpected fixture websocket event type %s", event.Type)
			}
		}
		t.Logf("WS synthetic response.create reply_type=%s error=%v observations=%v", event.Type, err, f.recorder.snapshot())
		return err
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if errClose := conn.Close(); errClose != nil {
			t.Errorf("close fixture websocket: %v", errClose)
		}
	}()
	body := `{"type":"response.create","model":"gpt-fixture-main","input":[]}`
	if err := send(conn, body); err != nil {
		t.Fatal(err)
	}
	f.recorder.failAccountA(http.StatusUnauthorized)
	errContinuation := send(conn, `{"type":"response.create","previous_response_id":"fixture-response","input":[]}`)
	var closeErr *websocket.CloseError
	if !errors.As(errContinuation, &closeErr) || closeErr.Code != websocket.CloseServiceRestart {
		t.Fatalf("expected typed session replay close, got %v", errContinuation)
	}
	replay, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if errClose := replay.Close(); errClose != nil {
			t.Errorf("close fixture replay websocket: %v", errClose)
		}
	}()
	if err := send(replay, body); err != nil {
		t.Fatal(err)
	}
	got := f.recorder.snapshot()
	if len(got) != 3 || got[0].Account != "fixture-a" || got[1].Account != "fixture-a" || got[2].Account != "fixture-b" {
		t.Fatalf("session observations=%v", got)
	}
	t.Logf("observed %v; session pin survives continuation but reconnect leaks to B without profile enforcement", got)
}

type routingHomeDispatcher struct{}

func (*routingHomeDispatcher) HeartbeatOK() bool       { return true }
func (*routingHomeDispatcher) AbortAmbiguousDispatch() {}
func (*routingHomeDispatcher) RPopAuth(context.Context, string, string, http.Header, int) ([]byte, error) {
	return json.Marshal(map[string]any{"auth": auth.Auth{ID: "fixture-b", Provider: "codex", Status: auth.StatusActive}})
}
func TestClientProfileFixtureHomeBypass(t *testing.T) {
	for _, kind := range []string{"execute", "stream", "count"} {
		t.Run(kind, func(t *testing.T) {
			f := newRoutingFixture(t)
			p := &routingPlugin{mode: "strict"}
			f.manager.SetPluginScheduler(p)
			f.manager.SetConfig(&internalconfig.Config{Home: internalconfig.HomeConfig{Enabled: true}})
			f.manager.PublishHomeDispatch(&routingHomeDispatcher{}, executionregistry.New(), 1)
			if err := f.invoke(kind, "gpt-fixture-main", ""); err != nil {
				t.Fatal(err)
			}
			if p.calls != 0 {
				t.Fatalf("Home invoked plugin %d times", p.calls)
			}
			got := f.recorder.snapshot()
			if len(got) != 1 || got[0].Account != "fixture-b" {
				t.Fatalf("Home observations=%v", got)
			}
			t.Logf("Home plugin_calls=0 observations=%v; strict plugin bypass observed", got)
		})
	}
}

func TestClientProfileFixtureCanary(t *testing.T) {
	f := newRoutingFixture(t)
	if err := f.invoke("execute", "gpt-fixture-main", "fixture-b"); err != nil {
		t.Fatal(err)
	}
	if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err == nil {
		t.Fatal("canary accepted wrong account B")
	}
	t.Log("canary rejected deliberately wrong account B")
}

func TestClientProfileFixtureBootstrapRetry(t *testing.T) {
	f := newRoutingFixture(t)
	f.recorder.bootstrapA = true
	h := handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{Streaming: sdkconfig.StreamingConfig{BootstrapRetries: 1}}, f.manager)
	ctx := handlers.WithPinnedAuthID(context.Background(), "fixture-a")
	data, _, errs := h.ExecuteStreamWithAuthManager(ctx, "openai", "gpt-fixture-helper", []byte(`{"model":"gpt-fixture-helper","input":[]}`), "")
	for range data {
	}
	failed := false
	for e := range errs {
		if e != nil && e.Error != nil {
			failed = true
		}
	}
	if !failed {
		t.Fatal("bootstrap A failure was hidden")
	}
	got := f.recorder.snapshot()
	if len(got) == 0 {
		t.Fatal("no upstream attempt")
	}
	if err := onlyAccount(got, "fixture-a"); err != nil {
		t.Fatal(err)
	}
	if err := f.invoke("stream", "gpt-fixture-helper", "fixture-b"); err != nil {
		t.Fatal(err)
	}
	t.Logf("bootstrap failure observations=%v; B separately succeeds", got)
}

func TestClientProfileFixtureHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newRoutingFixture(t)
	f.manager.SetPluginScheduler(&routingPlugin{mode: "strict"})
	h := openai.NewOpenAIResponsesAPIHandler(handlers.NewBaseAPIHandlers(&sdkconfig.SDKConfig{}, f.manager))
	router := gin.New()
	router.POST("/v1/responses", h.Responses)
	server := httptest.NewServer(router)
	defer server.Close()
	for _, unavailable := range []bool{false, true} {
		if unavailable {
			f.recorder.failAccountA(http.StatusServiceUnavailable)
		}
		resp, err := http.Post(server.URL+"/v1/responses", "application/json", strings.NewReader(`{"model":"gpt-fixture-helper","input":[]}`))
		if err != nil {
			t.Fatal(err)
		}
		status := resp.StatusCode
		if err := resp.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if (!unavailable && status != 200) || (unavailable && status != 503) {
			t.Fatalf("HTTP unavailable=%t status=%d", unavailable, status)
		}
		t.Logf("POST /v1/responses synthetic helper request unavailable=%t HTTP=%d observations=%v", unavailable, status, f.recorder.snapshot())
	}
	if err := onlyAccount(f.recorder.snapshot(), "fixture-a"); err != nil {
		t.Fatal(err)
	}
	f.manager.SetPluginScheduler(nil)
	if err := f.invoke("execute", "gpt-fixture-helper", "fixture-b"); err != nil {
		t.Fatalf("B available: %v", err)
	}
}
