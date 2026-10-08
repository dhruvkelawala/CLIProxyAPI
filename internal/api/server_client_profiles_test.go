package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type profileAPIFixture struct {
	server     *Server
	path       string
	manager    *coreauth.Manager
	credential string
	etag       string
}

func newProfileAPI(t *testing.T) *profileAPIFixture {
	t.Helper()
	t.Cleanup(func() { sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey) })
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("api-keys: [synthetic-client-key, synthetic-legacy-key]\nws-auth: true\nremote-management: {secret-key: synthetic-management}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AuthDir = dir
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(dir)
	manager := coreauth.NewManager(store, nil, nil)
	a, err := manager.Register(context.Background(), &coreauth.Auth{ID: "synthetic-a.json", FileName: "synthetic-a.json", Provider: "claude", Label: "Synthetic account A", Status: coreauth.StatusActive, Metadata: map[string]any{"type": "claude", "access_token": "synthetic-oauth-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	h := management.NewHandler(cfg, path, manager)
	h.SetLocalPassword("synthetic-management")
	s := &Server{cfg: cfg, engine: gin.New(), mgmt: h, accessManager: sdkaccess.NewManager()}
	s.managementRoutesEnabled.Store(true)
	s.registerManagementRoutes()
	s.applyAccessConfig(nil, cfg)
	h.SetClientProfilePublisher(func(next *config.Config) { s.applyAccessConfig(nil, next) })
	s.engine.GET("/synthetic-business", AuthMiddleware(s.accessManager), func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	f := &profileAPIFixture{server: s, path: path, manager: manager, credential: a.ID}
	f.request(t, "GET", "/client-profiles", "", 200)
	return f
}
func (f *profileAPIFixture) request(t *testing.T, method, path, body string, want int) map[string]any {
	t.Helper()
	req := httptest.NewRequest(method, "/v8/management"+path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer synthetic-management")
	req.Header.Set("Content-Type", "application/json")
	if f.etag != "" {
		req.Header.Set("If-Match", f.etag)
	}
	rec := httptest.NewRecorder()
	f.server.engine.ServeHTTP(rec, req)
	t.Logf("%s %s -> %d %s", method, path, rec.Code, rec.Body.String())
	if rec.Code != want {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
	}
	if etag := rec.Header().Get("ETag"); etag != "" {
		f.etag = etag
	}
	var result map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func (f *profileAPIFixture) business(t *testing.T, key string, want int) {
	t.Helper()
	req := httptest.NewRequest("GET", "/synthetic-business", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	f.server.engine.ServeHTTP(rec, req)
	t.Logf("GET /synthetic-business with synthetic credential -> %d %s", rec.Code, rec.Body.String())
	if rec.Code != want {
		t.Fatalf("business status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
	}
}
func (f *profileAPIFixture) enroll(t *testing.T) string {
	accounts := f.request(t, "GET", "/client-profile-accounts", "", 200)["accounts"].([]any)
	credential := accounts[0].(map[string]any)["credential_ref"].(string)
	return f.request(t, "POST", "/client-profile-accounts/enroll", fmt.Sprintf(`{"credential_ref":%q}`, credential), 200)["account_ref"].(string)
}
func TestManagementClientProfileContract(t *testing.T) {
	f := newProfileAPI(t)
	for _, path := range []string{"/client-profiles", "/client-profile-keys", "/client-profile-accounts", "/client-profiles/capabilities"} {
		req := httptest.NewRequest("GET", "/v8/management"+path, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		rec := httptest.NewRecorder()
		f.server.engine.ServeHTTP(rec, req)
		t.Logf("GET %s without management authentication -> %d", path, rec.Code)
		if rec.Code != 401 {
			t.Fatalf("unauthenticated %s=%d", path, rec.Code)
		}
	}
	if got := f.request(t, "GET", "/client-profiles/capabilities", "", 200); got["enforcement"] != false {
		t.Fatal(got)
	}
	ref := f.enroll(t)
	policies := fmt.Sprintf(`{"claude":{"mode":"only","account_ref":%q},"codex":{"mode":"automatic"}}`, ref)
	created := f.request(t, "POST", "/client-profiles", `{"label":"T3 Claude","policies":`+policies+`}`, 200)["result"].(map[string]any)
	profileRef := created["profile_ref"].(string)
	f.request(t, "POST", "/client-profiles/preview", fmt.Sprintf(`{"profile_ref":%q}`, profileRef), 200)
	key := f.request(t, "POST", "/client-profile-keys", fmt.Sprintf(`{"label":"T3 key","profile_ref":%q,"api_key":"synthetic-client-key"}`, profileRef), 200)["result"].(map[string]any)
	keyRef := key["key_ref"].(string)
	f.business(t, "synthetic-client-key", 503)
	f.business(t, "synthetic-legacy-key", 200)
	if profileRef == keyRef || profileRef == ref || keyRef == ref {
		t.Fatal("identities conflated")
	}
	listed := f.request(t, "GET", "/client-profiles", "", 200)
	safe, _ := json.Marshal(listed)
	for _, secret := range []string{"synthetic-client-key", "synthetic-legacy-key", "synthetic-oauth-secret", "access_token", "fingerprint", "synthetic-a.json"} {
		if strings.Contains(string(safe), secret) {
			t.Fatalf("projection leaked %s", secret)
		}
	}
	f.request(t, "DELETE", "/client-profiles/"+profileRef, "", 409)
	oldETag := f.etag
	f.request(t, "PUT", "/client-profile-keys/"+keyRef, `{"api_key":"synthetic-rotated-key"}`, 200)
	f.business(t, "synthetic-client-key", 401)
	f.business(t, "synthetic-rotated-key", 503)
	current := f.etag
	f.etag = oldETag
	f.request(t, "PUT", "/client-profiles/"+profileRef, `{"label":"stale","policies":`+policies+`}`, 412)
	f.etag = current
	f.request(t, "PUT", "/client-profiles/"+profileRef, `{"label":"Automatic","policies":{"claude":{"mode":"automatic"},"codex":{"mode":"automatic"}}}`, 200)
	f.business(t, "synthetic-rotated-key", 200)
	f.request(t, "DELETE", "/client-profile-keys/"+keyRef, "", 200)
	f.business(t, "synthetic-rotated-key", 401)
	f.request(t, "DELETE", "/client-profiles/"+profileRef, "", 200)
	loaded, err := config.LoadConfig(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ClientProfiles) != 0 || len(loaded.ClientProfileKeys) != 0 || len(loaded.APIKeys) != 1 || loaded.APIKeys[0] != "synthetic-legacy-key" {
		t.Fatalf("bad persisted configuration %+v", loaded.SDKConfig)
	}
}
func TestManagementClientProfileValidation(t *testing.T) {
	f := newProfileAPI(t)
	for _, policies := range []string{`{}`, `{"claude":{"mode":"automatic"}}`, `{"claude":{"mode":"prefer"},"codex":{"mode":"automatic"}}`, `{"other":{"mode":"automatic"},"codex":{"mode":"automatic"}}`, `{"claude":{"mode":"only","account_ref":"aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"},"codex":{"mode":"automatic"}}`} {
		f.request(t, "POST", "/client-profiles", `{"label":"bad","policies":`+policies+`}`, 422)
	}
	f.etag = ""
	f.request(t, "POST", "/client-profiles", `{"label":"missing revision"}`, 428)
	f.request(t, "PATCH", "/config/access/client-profiles", `[]`, 400)
	f.request(t, "GET", "/client-profiles", "", 200)
	ref := f.enroll(t)
	a, _ := f.manager.GetByID(f.credential)
	a.ID = "copy.json"
	a.FileName = "copy.json"
	a.Attributes = nil
	a.Disabled = true
	_, err := f.manager.Register(coreauth.WithSkipPersist(context.Background()), a)
	if err != nil {
		t.Fatal(err)
	}
	f.request(t, "POST", "/client-profiles", fmt.Sprintf(`{"label":"duplicate","policies":{"claude":{"mode":"only","account_ref":%q},"codex":{"mode":"automatic"}}}`, ref), 422)
	f.manager.Remove(context.Background(), f.credential)
	f.manager.Remove(context.Background(), a.ID)
	f.request(t, "POST", "/client-profiles", fmt.Sprintf(`{"label":"removed","policies":{"claude":{"mode":"only","account_ref":%q},"codex":{"mode":"automatic"}}}`, ref), 422)
}

type profileMutationReader struct {
	io.Reader
	mutate func()
	done   bool
}

func (r *profileMutationReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		r.mutate()
	}
	return r.Reader.Read(p)
}
func TestManagementClientProfileSaveFailureDoesNotPublish(t *testing.T) {
	f := newProfileAPI(t)
	p := f.request(t, "POST", "/client-profiles", `{"label":"Automatic","policies":{"claude":{"mode":"automatic"},"codex":{"mode":"automatic"}}}`, 200)["result"].(map[string]any)
	before, _ := os.ReadFile(f.path)
	req := httptest.NewRequest("POST", "/v8/management/client-profile-keys", &profileMutationReader{Reader: strings.NewReader(fmt.Sprintf(`{"label":"key","profile_ref":%q,"api_key":"synthetic-client-key"}`, p["profile_ref"])), mutate: func() {
		if err := os.WriteFile(f.path, append(before, []byte("\n# external edit\n")...), 0600); err != nil {
			t.Fatal(err)
		}
	}})
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer synthetic-management")
	req.Header.Set("If-Match", f.etag)
	rec := httptest.NewRecorder()
	f.server.engine.ServeHTTP(rec, req)
	if rec.Code != 412 {
		t.Fatalf("stale save status=%d body=%s", rec.Code, rec.Body.String())
	}
	f.business(t, "synthetic-client-key", 200)
	if got := f.request(t, "GET", "/client-profile-keys", "", 200)["keys"]; got != nil && len(got.([]any)) != 0 {
		t.Fatal("failed save published key")
	}
}

func TestManagementClientProfileReloadPending(t *testing.T) {
	f := newProfileAPI(t)
	p := f.request(t, "POST", "/client-profiles", `{"label":"Automatic","policies":{"claude":{"mode":"automatic"},"codex":{"mode":"automatic"}}}`, 200)["result"].(map[string]any)
	before, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(before), "label: Automatic", "label: External edit", 1)
	if changed == string(before) {
		t.Fatal("test failed to edit config")
	}
	if err = os.WriteFile(f.path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	f.request(t, "GET", "/client-profiles", "", 409)
	f.request(t, "PUT", "/client-profiles/"+p["profile_ref"].(string), `{"label":"overwrite","policies":{"claude":{"mode":"automatic"},"codex":{"mode":"automatic"}}}`, 409)
	after, _ := os.ReadFile(f.path)
	if string(after) != changed {
		t.Fatal("stale handler overwrote external edit")
	}
}
func TestManagementClientProfileMissingActivationOwner(t *testing.T) {
	f := newProfileAPI(t)
	p := f.request(t, "POST", "/client-profiles", `{"label":"Automatic","policies":{"claude":{"mode":"automatic"},"codex":{"mode":"automatic"}}}`, 200)["result"].(map[string]any)
	f.server.mgmt.SetClientProfilePublisher(nil)
	before, _ := os.ReadFile(f.path)
	f.request(t, "POST", "/client-profile-keys", fmt.Sprintf(`{"label":"key","profile_ref":%q,"api_key":"synthetic-client-key"}`, p["profile_ref"]), 503)
	after, _ := os.ReadFile(f.path)
	if string(after) != string(before) {
		t.Fatal("unactivated association persisted")
	}
	f.business(t, "synthetic-client-key", 200)
}

func TestManagementClientProfileUnsupportedDuplicate(t *testing.T) {
	f := newProfileAPI(t)
	ref := f.enroll(t)
	_, err := f.manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: "unsupported-copy", Provider: "unsupported-provider", Disabled: true, Metadata: map[string]any{"account_ref": ref}})
	if err != nil {
		t.Fatal(err)
	}
	result := f.request(t, "POST", "/client-profiles", fmt.Sprintf(`{"label":"duplicate","policies":{"claude":{"mode":"only","account_ref":%q},"codex":{"mode":"automatic"}}}`, ref), 422)
	if result["error"].(map[string]any)["code"] != "duplicate_account_ref" {
		t.Fatal("unsupported provider copy hidden", result)
	}
}

func TestManagementClientProfileConfigProjection(t *testing.T) {
	f := newProfileAPI(t)
	p := f.request(t, "POST", "/client-profiles", `{"label":"Automatic","policies":{"claude":{"mode":"automatic"},"codex":{"mode":"automatic"}}}`, 200)["result"].(map[string]any)
	f.request(t, "POST", "/client-profile-keys", fmt.Sprintf(`{"label":"key","profile_ref":%q,"api_key":"synthetic-client-key"}`, p["profile_ref"]), 200)
	req := httptest.NewRequest("GET", "/v8/management/config/access/client-profile-keys", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer synthetic-management")
	rec := httptest.NewRecorder()
	f.server.engine.ServeHTTP(rec, req)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "fingerprint") || strings.Contains(rec.Body.String(), "synthetic-client-key") {
		t.Fatal("unsafe JSON config projection", rec.Code, rec.Body.String())
	}
	f.request(t, "PUT", "/config/access/client-profile-keys", rec.Body.String(), 200)
	loaded, err := config.LoadConfig(f.path)
	if err != nil || len(loaded.ClientProfileKeys) != 1 || loaded.ClientProfileKeys[0].Fingerprint == "" {
		t.Fatal("projected roundtrip lost fingerprint", err)
	}
	f.request(t, "PUT", "/config/access/client-profile-keys", `[]`, 400)
}

func TestManagementClientProfileLastKeyDeletionFailsClosed(t *testing.T) {
	f := newProfileAPI(t)
	f.request(t, "PUT", "/config/access/api-keys", `["synthetic-client-key"]`, 200)
	f.request(t, "GET", "/client-profiles", "", 200)
	ref := f.enroll(t)
	p := f.request(t, "POST", "/client-profiles", fmt.Sprintf(`{"label":"Only","policies":{"claude":{"mode":"only","account_ref":%q},"codex":{"mode":"automatic"}}}`, ref), 200)["result"].(map[string]any)
	key := f.request(t, "POST", "/client-profile-keys", fmt.Sprintf(`{"label":"key","profile_ref":%q,"api_key":"synthetic-client-key"}`, p["profile_ref"]), 200)["result"].(map[string]any)
	result := f.request(t, "DELETE", "/client-profile-keys/"+key["key_ref"].(string), "", 409)
	if result["error"].(map[string]any)["code"] != "last_client_key" {
		t.Fatal(result)
	}
	f.business(t, "synthetic-client-key", 503)
}

func TestManagementClientProfileProgrammaticInvalidConfiguration(t *testing.T) {
	t.Cleanup(func() { sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey) })
	dir := t.TempDir()
	cfg, err := config.ParseConfigBytes([]byte("api-keys: [synthetic-client-key]\nws-auth: true\nremote-management: {secret-key: synthetic-management}\n"))
	if err != nil {
		t.Fatal(err)
	}
	ref := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	cfg.AuthDir = dir
	cfg.CommercialMode = true
	cfg.RemoteManagement.DisableControlPanel = true
	cfg.SDKConfig.ClientProfiles = []config.ClientProfile{{Ref: ref, Label: "invalid", Revision: 1, Policies: map[string]config.ClientProfilePolicy{"claude": {Mode: "automatic"}, "codex": {Mode: "automatic"}}}}
	cfg.SDKConfig.ClientProfileKeys = []config.ClientProfileKey{{Ref: ref, Label: "key", ProfileRef: ref, Fingerprint: clientprofiles.Fingerprint("synthetic-client-key"), Revision: 1}}
	cfg.WebsocketAuth = false
	s := NewServer(cfg, nil, nil, filepath.Join(dir, "config.yaml"), WithLocalManagementPassword("synthetic-management"))
	s.AttachWebsocketRoute("/synthetic-ws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for _, path := range []string{"/v1/models", "/synthetic-ws"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer synthetic-client-key")
		rec := httptest.NewRecorder()
		s.engine.ServeHTTP(rec, req)
		if rec.Code != 503 {
			t.Fatalf("invalid programmatic %s status=%d", path, rec.Code)
		}
	}
}

func TestManagementClientProfileProgrammaticRepairAndLegacy(t *testing.T) {
	t.Cleanup(func() { sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey) })
	dir := t.TempDir()
	cfg, err := config.ParseConfigBytes([]byte("api-keys: [synthetic-client-key]\nws-auth: true\nremote-management: {secret-key: synthetic-management}\n"))
	if err != nil {
		t.Fatal(err)
	}
	ref := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	cfg.AuthDir = dir
	cfg.CommercialMode = true
	cfg.RemoteManagement.DisableControlPanel = true
	cfg.ClientProfiles = []config.ClientProfile{{Ref: ref, Label: "app", Revision: 1, Policies: map[string]config.ClientProfilePolicy{"claude": {Mode: "automatic"}, "codex": {Mode: "automatic"}}}}
	cfg.ClientProfileKeys = []config.ClientProfileKey{{Ref: ref, Label: "key", ProfileRef: ref, Fingerprint: clientprofiles.Fingerprint("synthetic-client-key"), Revision: 1}}
	cfg.WebsocketAuth = false
	s := NewServer(cfg, nil, nil, filepath.Join(dir, "config.yaml"), WithLocalManagementPassword("synthetic-management"))
	s.AttachWebsocketRoute("/synthetic-ws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	check := func(path string, want int) {
		t.Helper()
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		key := "synthetic-client-key"
		if strings.HasPrefix(path, "/v8/management") {
			key = "synthetic-management"
		}
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		s.engine.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s=%d want=%d %s", path, rec.Code, want, rec.Body.String())
		}
	}
	check("/synthetic-ws", 503)
	check("/v8/management/client-profiles/capabilities", 200)
	check("/management.html", 404)
	valid := cfg.CloneForRuntime()
	valid.WebsocketAuth = true
	if !s.UpdateClientsContext(context.Background(), valid) {
		t.Fatal("valid repair rejected")
	}
	check("/synthetic-ws", 200)
	if s.UpdateClientsContext(context.Background(), cfg) {
		t.Fatal("invalid reload accepted")
	}
	check("/synthetic-ws", 200)
	legacy := valid.CloneForRuntime()
	legacy.ClientProfiles = nil
	legacy.ClientProfileKeys = nil
	legacy.WebsocketAuth = false
	legacyServer := NewServer(legacy, nil, nil, filepath.Join(dir, "legacy.yaml"))
	legacyServer.AttachWebsocketRoute("/legacy-ws", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	req := httptest.NewRequest("GET", "/legacy-ws", nil)
	rec := httptest.NewRecorder()
	legacyServer.engine.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal("legacy nil-owner behavior changed", rec.Code)
	}
}

func TestManagementClientProfileUnsupportedTargets(t *testing.T) {
	for _, kind := range []string{"config_api_key", "plugin_virtual", "runtime_only", "runtime_only_upper", "runtime_only_spaced", "runtime_only_mixed", "custom_storage", "no_store"} {
		t.Run(kind, func(t *testing.T) {
			f := newProfileAPI(t)
			ref := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
			a := &coreauth.Auth{ID: "unsupported", FileName: "unsupported.json", Provider: "claude", Status: coreauth.StatusActive, Metadata: map[string]any{"account_ref": ref}}
			if err := os.WriteFile(filepath.Join(filepath.Dir(f.path), "unsupported.json"), []byte(fmt.Sprintf(`{"type":"claude","account_ref":%q}`, ref)), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "config_api_key":
				a.Attributes = map[string]string{"source": "config:claude[synthetic]", "api_key": "synthetic-upstream-key"}
			case "plugin_virtual":
				coreauth.MarkPluginVirtualAuth(a, "synthetic.json", 0)
			case "runtime_only", "runtime_only_upper", "runtime_only_spaced", "runtime_only_mixed":
				value := map[string]string{"runtime_only": "true", "runtime_only_upper": "TRUE", "runtime_only_spaced": " TRUE ", "runtime_only_mixed": "TrUe"}[kind]
				a.Attributes = map[string]string{"runtime_only": value}
			case "custom_storage":
				a.Storage = &profileUnsupportedStorage{}
			case "no_store":
				f.manager = coreauth.NewManager(nil, nil, nil)
				f.server.mgmt.SetAuthManager(f.manager)
			}
			if _, err := f.manager.Register(coreauth.WithSkipPersist(context.Background()), a); err != nil {
				t.Fatal(err)
			}
			inventory := f.request(t, "GET", "/client-profile-accounts", "", 200)["accounts"].([]any)
			for _, raw := range inventory {
				account := raw.(map[string]any)
				if account["account_ref"] == ref && (account["enrollment_supported"] != false || account["target_supported"] != false) {
					t.Fatal("unsupported imported credential advertised", account)
				}
			}
			result := f.request(t, "POST", "/client-profiles", fmt.Sprintf(`{"label":"unsupported","policies":{"claude":{"mode":"only","account_ref":%q},"codex":{"mode":"automatic"}}}`, ref), 422)
			if result["error"].(map[string]any)["code"] != "target_unsupported" {
				t.Fatal("unsupported credential accepted or hidden", result)
			}
		})
	}
}

type profileUnsupportedStorage struct{}

func (*profileUnsupportedStorage) SaveTokenToFile(string) error { return nil }
