package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
)

func TestClientProfileEnrollmentRollback(t *testing.T) {
	manager := NewManager(&enrollmentFailureStore{}, nil, nil)
	a, err := manager.Register(WithSkipPersist(context.Background()), &Auth{ID: "synthetic", Provider: "claude", Metadata: map[string]any{"type": "claude"}})
	if err != nil {
		t.Fatal(err)
	}
	updated := a.Clone()
	updated.Metadata["note"] = "ordinary update publishes on save error"
	if _, err = manager.Update(context.Background(), updated); err != nil {
		t.Fatal("ordinary update behavior changed", err)
	}
	current, _ := manager.GetByID(a.ID)
	if current.Metadata["note"] != updated.Metadata["note"] {
		t.Fatal("expected existing nonfatal Update semantics")
	}
	if _, err = manager.EnrollAccountReference(context.Background(), a.ID); err == nil {
		t.Fatal("enrollment ignored failed persistence")
	}
	current, _ = manager.GetByID(a.ID)
	if _, ok := current.Metadata[clientprofiles.AccountRefMetadataKey]; ok {
		t.Fatal("failed enrollment published UUID")
	}
}
func TestClientProfileRefreshPreservesReference(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	ref := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	a, err := manager.Register(WithSkipPersist(context.Background()), &Auth{ID: "synthetic", Provider: "claude", Metadata: map[string]any{"type": "claude", clientprofiles.AccountRefMetadataKey: ref}})
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{"", "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"} {
		updated := a.Clone()
		updated.Metadata = map[string]any{"type": "claude", "access_token": "new-synthetic"}
		if replacement != "" {
			updated.Metadata[clientprofiles.AccountRefMetadataKey] = replacement
		}
		a, err = manager.UpdateRefreshedAuth(WithSkipPersist(context.Background()), a, updated)
		if err != nil {
			t.Fatal(err)
		}
		if a.Metadata[clientprofiles.AccountRefMetadataKey] != ref {
			t.Fatal("refresh changed account reference")
		}
	}
}

type enrollmentFailureStore struct{ persistFailureStore }

func (s *enrollmentFailureStore) SaveEnrollment(context.Context, *Auth) (string, error) {
	return "", errors.New("synthetic enrollment save failure")
}

func TestClientProfileRuntimeOnlyEnrollmentRejectsWithoutPublication(t *testing.T) {
	for _, value := range []string{"true", "TRUE", " TRUE ", "TrUe", "\ttrue\n"} {
		t.Run(value, func(t *testing.T) {
			store := &runtimeOnlyEnrollmentStore{}
			manager := NewManager(store, nil, nil)
			a, err := manager.Register(WithSkipPersist(context.Background()), &Auth{ID: "synthetic-runtime", Provider: "claude", Metadata: map[string]any{"type": "claude"}, Attributes: map[string]string{"runtime_only": value}})
			if err != nil {
				t.Fatal(err)
			}
			if manager.SupportsAccountEnrollment(a.ID) {
				t.Fatal("runtime-only enrollment advertised")
			}
			if enrolled, err := manager.EnrollAccountReference(context.Background(), a.ID); err == nil || enrolled != nil {
				t.Fatal("runtime-only enrollment succeeded", enrolled, err)
			}
			current, _ := manager.GetByID(a.ID)
			if _, ok := current.Metadata[clientprofiles.AccountRefMetadataKey]; ok {
				t.Fatal("runtime-only UUID published")
			}
			if store.calls != 0 {
				t.Fatalf("runtime-only store calls=%d", store.calls)
			}
		})
	}
}

type runtimeOnlyEnrollmentStore struct {
	persistFailureStore
	calls int
}

func (s *runtimeOnlyEnrollmentStore) SaveEnrollment(context.Context, *Auth) (string, error) {
	s.calls++
	return "synthetic-saved", nil
}
