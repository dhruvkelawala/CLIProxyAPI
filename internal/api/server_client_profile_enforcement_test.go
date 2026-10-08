package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
)

type opaqueRealtimeProfileProvider struct{}

func (*opaqueRealtimeProfileProvider) Identifier() string { return "synthetic-opaque-provider" }
func (*opaqueRealtimeProfileProvider) Authenticate(_ context.Context, r *http.Request) (*sdkaccess.Result, *sdkaccess.AuthError) {
	if r.Header.Get("Authorization") != "Bearer synthetic-plugin-token" {
		return nil, sdkaccess.NewInvalidCredentialError()
	}
	return &sdkaccess.Result{Principal: "opaque-user-id", Provider: "synthetic-opaque-provider"}, nil
}
func TestClientProfileEnforcementRealtimeIssuer(t *testing.T) {
	for _, mode := range []string{"associated", "rotated", "deleted", "opaque-legacy"} {
		t.Run(mode, func(t *testing.T) {
			s := newTestServer(t)
			t.Cleanup(func() { sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey) })
			if mode == "opaque-legacy" {
				s.accessManager.SetProviders([]sdkaccess.Provider{&opaqueRealtimeProfileProvider{}})
			}
			key := "test-key"
			if mode == "opaque-legacy" {
				key = "synthetic-plugin-token"
			}
			mint := httptest.NewRequest("POST", "/v1/realtime/client_secrets", strings.NewReader(`{"session":{"type":"realtime","model":"gpt-realtime"}}`))
			mint.Header.Set("Authorization", "Bearer "+key)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, mint)
			if rec.Code != 200 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			var secret struct {
				Value string `json:"value"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &secret); err != nil || secret.Value == "" {
				t.Fatal("synthetic secret was not issued", err)
			}
			if mode != "opaque-legacy" {
				cfg := s.cfg.CloneForRuntime()
				ref := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
				cfg.WebsocketAuth = true
				cfg.ClientProfiles = []clientprofiles.Profile{{Ref: ref, Label: "synthetic", Revision: 1, Policies: map[string]clientprofiles.Policy{"claude": {Mode: "automatic"}, "codex": {Mode: "automatic"}}}}
				cfg.ClientProfileKeys = []clientprofiles.Key{{Ref: ref, ProfileRef: ref, Label: "synthetic", Revision: 1, Fingerprint: clientprofiles.Fingerprint(key)}}
				if !s.UpdateClientsContext(context.Background(), cfg) {
					t.Fatal("association reload failed")
				}
				if mode == "rotated" {
					next := cfg.CloneForRuntime()
					next.APIKeys = []string{"synthetic-replacement"}
					next.ClientProfileKeys[0].Fingerprint = clientprofiles.Fingerprint("synthetic-replacement")
					next.ClientProfileKeys[0].Revision++
					if !s.UpdateClientsContext(context.Background(), next) {
						t.Fatal("rotation reload failed")
					}
				}
				if mode == "deleted" {
					next := cfg.CloneForRuntime()
					next.APIKeys = []string{"synthetic-other"}
					next.ClientProfileKeys = nil
					if !s.UpdateClientsContext(context.Background(), next) {
						t.Fatal("deletion reload failed")
					}
				}
			}
			call := httptest.NewRequest("GET", "/v1/realtime?model=gpt-realtime", nil)
			call.Header.Set("Authorization", "Bearer "+secret.Value)
			reply := httptest.NewRecorder()
			s.Handler().ServeHTTP(reply, call)
			if mode == "opaque-legacy" {
				if reply.Code != http.StatusUpgradeRequired {
					t.Fatal("opaque legacy issuer changed", reply.Code, reply.Body.String())
				}
			} else if reply.Code != 503 && reply.Code != 401 {
				t.Fatal("old issuer was accepted", reply.Code, reply.Body.String())
			}
			t.Logf("realtime issuer %s -> %d %s", mode, reply.Code, reply.Body.String())
		})
	}
}

func TestClientProfileEnforcementOwnerLoss(t *testing.T) {
	s := newTestServer(t)
	t.Cleanup(func() { sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey) })
	cfg := s.cfg.CloneForRuntime()
	ref := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	cfg.WebsocketAuth = true
	cfg.ClientProfiles = []clientprofiles.Profile{{Ref: ref, Label: "synthetic", Revision: 1, Policies: map[string]clientprofiles.Policy{"claude": {Mode: "only", AccountRef: ref}, "codex": {Mode: "automatic"}}}}
	cfg.ClientProfileKeys = []clientprofiles.Key{{Ref: ref, Label: "synthetic", ProfileRef: ref, Revision: 1, Fingerprint: clientprofiles.Fingerprint("test-key")}}
	if !s.UpdateClientsContext(context.Background(), cfg) {
		t.Fatal("profile reload failed")
	}
	s.accessManager.SetProvidersAndCredentialOwner([]sdkaccess.Provider{&shipExclusiveFrontendProvider{}}, nil)
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "profile_owner_unavailable") {
		t.Fatal(rec.Code, rec.Body.String())
	}
	t.Logf("actual server configured owner loss -> %d %s", rec.Code, rec.Body.String())
}

func TestClientProfileEnforcementOptionalRelay(t *testing.T) {
	s := newTestServer(t)
	t.Cleanup(func() { sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey) })
	calls := 0
	s.AttachWebsocketRoute("/synthetic-relay", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(200) }))
	cfg := s.cfg.CloneForRuntime()
	ref := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	cfg.WebsocketAuth = true
	cfg.APIKeys = append(cfg.APIKeys, "synthetic-legacy")
	cfg.ClientProfileKeys = []clientprofiles.Key{{Ref: ref, Label: "synthetic", ProfileRef: ref, Revision: 1, Fingerprint: clientprofiles.Fingerprint("test-key")}}
	for _, mode := range []string{"automatic", "only"} {
		policy := clientprofiles.Policy{Mode: mode}
		if mode == "only" {
			policy.AccountRef = ref
		}
		cfg.ClientProfiles = []clientprofiles.Profile{{Ref: ref, Label: "synthetic", Revision: 1, Policies: map[string]clientprofiles.Policy{"claude": policy, "codex": {Mode: "automatic"}}}}
		if !s.UpdateClientsContext(context.Background(), cfg) {
			t.Fatal("reload failed")
		}
		req := httptest.NewRequest("GET", "/synthetic-relay", nil)
		req.Header.Set("Authorization", "Bearer test-key")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != 503 || calls != 0 || !strings.Contains(rec.Body.String(), "profile_protocol_unsupported") {
			t.Fatal(rec.Code, calls, rec.Body.String())
		}
		t.Logf("bound %s relay -> %d %s; attached-handler calls=%d", mode, rec.Code, rec.Body.String(), calls)
	}
	req := httptest.NewRequest("GET", "/synthetic-relay", nil)
	req.Header.Set("Authorization", "Bearer synthetic-legacy")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 || calls != 1 {
		t.Fatal("legacy relay changed", rec.Code, calls)
	}
}
