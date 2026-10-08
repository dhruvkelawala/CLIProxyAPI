package clientprofiles

import "testing"

func TestClientProfileValidation(t *testing.T) {
	ref := "6ea741d4-c122-4c46-924b-c93f15a99f39"
	p := Profile{Ref: ref, Label: "Claude app", Policies: map[string]Policy{"claude": {Mode: "only", AccountRef: ref}, "codex": {Mode: "automatic"}}}
	if err := Validate([]Profile{p}, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []Policy{{}, {Mode: "prefer"}, {Mode: "only"}, {Mode: "automatic", AccountRef: ref}} {
		p.Policies["claude"] = policy
		if Validate([]Profile{p}, nil, nil) == nil {
			t.Fatalf("accepted %+v", policy)
		}
	}
	p.Policies = map[string]Policy{"unknown": {Mode: "automatic"}}
	if Validate([]Profile{p}, nil, nil) == nil {
		t.Fatal("unknown provider accepted")
	}
	p.Policies = nil
	if Validate([]Profile{p}, nil, nil) == nil {
		t.Fatal("missing policies accepted")
	}
}
func TestClientProfileAccountResolution(t *testing.T) {
	ref := "6ea741d4-c122-4c46-924b-c93f15a99f39"
	policy := Policy{Mode: "only", AccountRef: ref}
	a := Account{AccountRef: ref, Provider: "claude", Available: true, TargetSupported: true}
	if Resolve("claude", policy, []Account{a}) != "available" {
		t.Fatal("valid target unavailable")
	}
	b := a
	b.Available = false
	if Resolve("claude", policy, []Account{a, b}) != "duplicate_account_ref" {
		t.Fatal("disabled copy not counted")
	}
	if Resolve("codex", policy, []Account{a}) != "provider_mismatch" {
		t.Fatal("wrong provider accepted")
	}
	if Resolve("claude", policy, nil) != "target_removed" {
		t.Fatal("missing target accepted")
	}
}

func TestClientProfileBindingSnapshot(t *testing.T) {
	ref := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	profiles := []Profile{{Ref: ref, Label: "app", Revision: 1, Policies: map[string]Policy{"claude": {Mode: "only", AccountRef: ref}, "codex": {Mode: "automatic"}}}}
	keys := []Key{{Ref: "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb", Label: "key", ProfileRef: ref, Fingerprint: Fingerprint("synthetic"), Revision: 1}}
	snapshot, err := Binding(profiles, keys, "synthetic")
	if err != nil || !snapshot.Bound {
		t.Fatal(snapshot, err)
	}
	snapshot.Policies["claude"] = Policy{Mode: "automatic"}
	if profiles[0].Policies["claude"].Mode != "only" {
		t.Fatal("snapshot shared provider map")
	}
	broken, err := Binding(nil, keys, "synthetic")
	if err == nil || !broken.Bound {
		t.Fatal("broken owner fell back to legacy", broken, err)
	}
	legacy, err := Binding(profiles, keys, "unassociated")
	if err != nil || legacy.Bound {
		t.Fatal("legacy key changed", legacy, err)
	}
}
