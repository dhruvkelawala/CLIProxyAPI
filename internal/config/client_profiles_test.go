package config

import (
	"reflect"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	"gopkg.in/yaml.v3"
)

func TestClientProfileConfigRoundTrip(t *testing.T) {
	cfg, err := ParseConfigBytes([]byte("api-keys: [synthetic-key]\nws-auth: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	ref := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	cfg.ClientProfiles = []clientprofiles.Profile{{Ref: ref, Label: "app", Revision: 1, Policies: map[string]clientprofiles.Policy{"claude": {Mode: "only", AccountRef: ref}, "codex": {Mode: "automatic"}}}}
	cfg.ClientProfileKeys = []clientprofiles.Key{{Ref: ref, Label: "key", ProfileRef: ref, Fingerprint: clientprofiles.Fingerprint("synthetic-key"), Revision: 1}}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	v8, _, err := NormalizeConfigLayout(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(v8), "client-profile-keys:") {
		t.Fatal(string(v8))
	}
	if err = ValidateV8Config(v8); err != nil {
		t.Fatal(err)
	}
	loaded, err := ParseConfigBytes(v8)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.ClientProfiles, cfg.ClientProfiles) || !reflect.DeepEqual(loaded.ClientProfileKeys, cfg.ClientProfileKeys) || !reflect.DeepEqual(loaded.APIKeys, cfg.APIKeys) {
		t.Fatal("roundtrip lost profiles/keys")
	}
	clone := cfg.CloneForRuntime()
	clone.ClientProfiles[0].Policies["claude"] = clientprofiles.Policy{Mode: "automatic"}
	clone.ClientProfileKeys[0].Label = "changed"
	if cfg.ClientProfiles[0].Policies["claude"].Mode != "only" || cfg.ClientProfileKeys[0].Label != "key" {
		t.Fatal("shared clone")
	}
	var doc yaml.Node
	if err = yaml.Unmarshal(v8, &doc); err != nil {
		t.Fatal(err)
	}
	deleteYAMLPath(doc.Content[0], "oauth.providers.aistudio")
	withoutWS, err := yaml.Marshal(&doc)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateV8Config(withoutWS); err != nil {
		t.Fatal("default websocket auth rejected", err)
	}
	cfg.WebsocketAuth = false
	if cfg.ValidateClientProfiles() == nil {
		t.Fatal("accepted unauthenticated websocket binding")
	}
}

func TestClientProfileRevocationValidation(t *testing.T) {
	key := clientprofiles.Fingerprint("synthetic-revoked")
	cfg := &Config{WebsocketAuth: true}
	cfg.RevokedClientProfileKeys = []string{key}
	cfg.ClientProfileKeys = []clientprofiles.Key{{Fingerprint: key}}
	if err := cfg.ValidateClientProfiles(); err == nil || err.(*clientprofiles.Error).Code != "revoked_key_bound" {
		t.Fatal("active revoked collision allowed", err)
	}
	for _, tc := range []struct {
		name    string
		revoked []string
		ws      bool
	}{
		{"canonical", []string{key}, true},
		{"short", []string{"abc"}, true},
		{"uppercase", []string{strings.ToUpper(key)}, true},
		{"duplicate", []string{key, key}, true},
		{"websocket-disabled", []string{key}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{WebsocketAuth: tc.ws}
			cfg.RevokedClientProfileKeys = tc.revoked
			err := cfg.ValidateClientProfiles()
			if (err == nil) != (tc.name == "canonical") {
				t.Fatal(err)
			}
		})
	}
}
