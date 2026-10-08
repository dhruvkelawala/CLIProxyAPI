package test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	auth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestClientProfileEnforcementCallerAffinity(t *testing.T) {
	f := newEnforcementFixture(t)
	const refB = "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	const keyB = "synthetic-only-b-key"
	f.cfg.ClientProfiles[0].Policies["codex"] = clientprofiles.Policy{Mode: "automatic"}
	f.cfg.ClientProfiles = append(f.cfg.ClientProfiles, clientprofiles.Profile{Ref: refB, Label: "Only B", Revision: 1, Policies: map[string]clientprofiles.Policy{"codex": {Mode: "only", AccountRef: refB}, "claude": {Mode: "automatic"}}})
	f.cfg.ClientProfileKeys = append(f.cfg.ClientProfileKeys, clientprofiles.Key{Ref: refB, Label: "Only B", ProfileRef: refB, Revision: 1, Fingerprint: clientprofiles.Fingerprint(keyB)})
	f.cfg.APIKeys = append(f.cfg.APIKeys, keyB)
	a, _ := f.manager.GetByID("fixture-a")
	a.Attributes["priority"] = "100"
	if _, err := f.manager.Update(auth.WithSkipPersist(context.Background()), a); err != nil {
		t.Fatal(err)
	}
	b, _ := f.manager.GetByID("fixture-b")
	b.Metadata[clientprofiles.AccountRefMetadataKey] = refB
	if _, err := f.manager.Update(auth.WithSkipPersist(context.Background()), b); err != nil {
		t.Fatal(err)
	}
	f.manager.SetConfig(f.cfg)
	selector := auth.NewSessionAffinitySelector(&auth.RoundRobinSelector{})
	defer selector.Stop()
	f.manager.SetSelector(selector)
	server := f.server(t)
	for _, step := range []struct{ label, key, want string }{
		{"Automatic", enforcementKey, "fixture-a"},
		{"Only B", keyB, "fixture-b"},
		{"Automatic continued", enforcementKey, "fixture-a"},
		{"Legacy", "synthetic-legacy-key", "fixture-a"},
		{"Only B continued", keyB, "fixture-b"},
		{"Legacy continued", "synthetic-legacy-key", "fixture-a"},
	} {
		before := len(f.recorder.snapshot())
		req, err := http.NewRequest("POST", server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-fixture-main","input":[],"metadata":{"user_id":"shared-conversation"}}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+step.key)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		if errClose := resp.Body.Close(); errClose != nil {
			t.Fatal(errClose)
		}
		if err != nil {
			t.Fatal(err)
		}
		got := f.recorder.snapshot()[before:]
		t.Logf("POST /v1/responses client=%s session=shared-conversation status=%d response=%s upstream=%v", step.label, resp.StatusCode, string(body), got)
		if resp.StatusCode != http.StatusOK || len(got) != 1 || got[0].Account != step.want {
			t.Errorf("client %s moved conversation: want %s, status=%d observations=%v", step.label, step.want, resp.StatusCode, got)
		}
	}
}
