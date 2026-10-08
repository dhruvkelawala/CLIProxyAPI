package auth

import (
	"context"
	"net/http"
	"testing"

	executor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func TestSessionAffinityCallerIsolation(t *testing.T) {
	for _, signal := range []string{"header", "body", "hash"} {
		t.Run(signal, func(t *testing.T) {
			s := NewSessionAffinitySelector(&RoundRobinSelector{})
			defer s.Stop()
			if signal == "hash" {
				s.matcher = nil
			}
			a := &Auth{ID: "a", Provider: "claude", Status: StatusActive}
			b := &Auth{ID: "b", Provider: "claude", Status: StatusActive}
			options := func(scope string) executor.Options {
				opts := executor.Options{Metadata: map[string]any{executor.CallerScopeMetadataKey: scope}}
				switch signal {
				case "header":
					opts.Headers = http.Header{"X-Claude-Code-Session-Id": {"shared"}}
				case "body":
					opts.OriginalRequest = []byte(`{"metadata":{"user_id":"shared"}}`)
				case "hash":
					opts.OriginalRequest = []byte(`{"messages":[{"role":"user","content":"same initial message"}]}`)
				}
				return opts
			}
			pick := func(scope, want string, auths []*Auth) executor.Options {
				t.Helper()
				opts := options(scope)
				got, err := s.Pick(context.Background(), "claude", "model", opts, auths)
				if err != nil || got == nil || got.ID != want {
					t.Fatalf("caller=%q want=%s got=%v err=%v", scope, want, got, err)
				}
				return opts
			}
			optsA := pick("client-a", "a", []*Auth{a})
			optsB := pick("client-b", "b", []*Auth{b})
			pick("client-a", "a", []*Auth{a, b})
			s.OnResult(Result{AuthID: "b", Provider: "claude", Model: "model", Success: false, Error: &Error{Code: "upstream", HTTPStatus: 500}, Options: optsB})
			pick("client-a", "a", []*Auth{a, b})
			s.OnResult(Result{AuthID: "a", Provider: "claude", Model: "model", Success: true, Options: optsA})
			pick("client-b", "b", []*Auth{b})
			pick("client-a", "a", []*Auth{a, b})
		})
	}
}

func TestSessionAffinityCallerParentIsolation(t *testing.T) {
	s := NewSessionAffinitySelector(&RoundRobinSelector{})
	defer s.Stop()
	a := &Auth{ID: "a", Provider: "claude", Status: StatusActive}
	b := &Auth{ID: "b", Provider: "claude", Status: StatusActive}
	pick := func(scope, agent, want string, auths []*Auth) {
		t.Helper()
		headers := http.Header{"X-Claude-Code-Session-Id": {"parent"}}
		if agent != "" {
			headers.Set("X-Claude-Code-Agent-Id", agent)
		}
		opts := executor.Options{Headers: headers, Metadata: map[string]any{executor.CallerScopeMetadataKey: scope}}
		got, err := s.Pick(context.Background(), "claude", "model", opts, auths)
		if err != nil || got == nil || got.ID != want {
			t.Fatalf("caller=%s agent=%s want=%s got=%v err=%v", scope, agent, want, got, err)
		}
	}
	pick("client-a", "", "a", []*Auth{a})
	pick("client-b", "", "b", []*Auth{b})
	pick("client-a", "child", "a", []*Auth{a, b})
	pick("client-b", "child", "b", []*Auth{a, b})
}

func TestSessionAffinityCallerFailureClearsOnlyOwnBinding(t *testing.T) {
	s := NewSessionAffinitySelector(&RoundRobinSelector{})
	defer s.Stop()
	a := &Auth{ID: "a", Provider: "claude", Status: StatusActive}
	b := &Auth{ID: "b", Provider: "claude", Status: StatusActive}
	pick := func(scope, want string, auths []*Auth) executor.Options {
		t.Helper()
		opts := executor.Options{Headers: http.Header{"X-Claude-Code-Session-Id": {"shared"}}, Metadata: map[string]any{executor.CallerScopeMetadataKey: scope}}
		got, err := s.Pick(context.Background(), "claude", "model", opts, auths)
		if err != nil || got == nil || got.ID != want {
			t.Fatalf("caller=%s want=%s got=%v err=%v", scope, want, got, err)
		}
		return opts
	}
	pick("client-a", "a", []*Auth{a})
	optsB := pick("client-b", "a", []*Auth{a})
	s.OnResult(Result{AuthID: "a", Provider: "claude", Model: "model", Error: &Error{Code: "upstream", HTTPStatus: 500}, Options: optsB})
	pick("client-b", "b", []*Auth{a, b})
	pick("client-a", "a", []*Auth{a, b})
}

func TestSessionAffinityLookupIndependentCallers(t *testing.T) {
	s := NewSessionAffinitySelector(&RoundRobinSelector{})
	defer s.Stop()
	for _, id := range []string{"a", "b"} {
		opts := executor.Options{Headers: http.Header{"X-Claude-Code-Session-Id": {"shared"}}, Metadata: map[string]any{executor.CallerScopeMetadataKey: id}}
		if _, err := s.Pick(context.Background(), "claude", "model", opts, []*Auth{{ID: id, Provider: "claude", Status: StatusActive}}); err != nil {
			t.Fatal(err)
		}
	}
	if id, status := s.LookupAffinity("claude", "model", "shared"); id != "" || status != "ambiguous" {
		t.Fatalf("independent caller bindings: id=%s status=%s", id, status)
	}
}
