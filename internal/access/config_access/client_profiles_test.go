package configaccess

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestClientProfileAccessReload(t *testing.T) {
	defer sdkaccess.UnregisterProvider(sdkaccess.AccessProviderTypeConfigAPIKey)
	ref := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	cfg := &sdkconfig.SDKConfig{APIKeys: []string{"synthetic-key"}, ClientProfiles: []clientprofiles.Profile{{Ref: ref, Label: "test", Revision: 1, Policies: map[string]clientprofiles.Policy{"claude": {Mode: "only", AccountRef: ref}, "codex": {Mode: "automatic"}}}}, ClientProfileKeys: []clientprofiles.Key{{Ref: ref, ProfileRef: ref, Fingerprint: clientprofiles.Fingerprint("synthetic-key"), Revision: 1}}}
	check := func(want int) {
		t.Helper()
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Authorization", "Bearer synthetic-key")
		providers := sdkaccess.RegisteredProviders()
		if len(providers) != 1 {
			t.Fatalf("providers %d", len(providers))
		}
		result, err := providers[0].Authenticate(context.Background(), req)
		if want == 200 {
			if err != nil || result.Metadata["client_profile_binding"] == "" {
				t.Fatalf("automatic binding result=%+v err=%v", result, err)
			}
		} else if err == nil || err.HTTPStatusCode() != want {
			t.Fatalf("error=%v want=%d", err, want)
		}
	}
	Register(cfg)
	cfg.ClientProfiles[0].Policies["claude"] = clientprofiles.Policy{Mode: "automatic"}
	check(200)
	Register(cfg)
	check(200)
	cfg.ClientProfiles = nil
	Register(cfg)
	check(503)
	cfg.ClientProfileKeys = nil
	Register(cfg)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer synthetic-key")
	result, err := sdkaccess.RegisteredProviders()[0].Authenticate(context.Background(), req)
	if err != nil || result.Principal != "synthetic-key" {
		t.Fatal(result, err)
	}
}
