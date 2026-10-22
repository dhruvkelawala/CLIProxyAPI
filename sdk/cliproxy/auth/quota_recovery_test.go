package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
)

func TestQuotaRecoveryDiscardsStaleObservations(t *testing.T) {
	for _, change := range []string{"new rejection", "credential replaced", "cancelled lookup"} {
		t.Run(change, func(t *testing.T) {
			manager := NewManager(nil, nil, nil)
			id := uuid.NewString()
			ctx := context.Background()
			if _, err := manager.Register(ctx, &Auth{ID: id, Provider: "claude"}); err != nil {
				t.Fatal(err)
			}
			retry := 7 * 24 * time.Hour
			failure := Result{AuthID: id, Provider: "claude", Model: "opus", CredentialScope: true, RetryAfter: &retry, Error: &Error{HTTPStatus: http.StatusTooManyRequests}}
			manager.MarkResult(ctx, failure)
			observation, _ := manager.GetByID(id)
			switch change {
			case "new rejection":
				manager.MarkResult(ctx, failure)
			case "credential replaced":
				if _, err := manager.Register(ctx, &Auth{ID: id, Provider: "claude"}); err != nil {
					t.Fatal(err)
				}
				manager.MarkResult(ctx, failure)
			case "cancelled lookup":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			updated, _, err := manager.resetQuota(ctx, id, observation)
			if err != nil {
				t.Fatal(err)
			}
			current, _ := manager.GetByID(id)
			if updated != nil || !current.Quota.Exceeded || !current.Unavailable {
				t.Fatalf("stale usage observation cleared a newer cooldown: updated=%v unavailable=%v quota=%v", updated != nil, current.Unavailable, current.Quota.Exceeded)
			}
		})
	}
}

func TestQuotaRecoveryPreservesUnrelatedModelFailuresAndPersistsRecovery(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	store := &recordingCooldownStateStore{}
	manager.SetCooldownStateStore(store)
	ctx := context.Background()
	id := uuid.NewString()
	const availableModel = "quota-recovered-opus"
	const failedModel = "quota-recovered-sonnet"
	reg := registry.GetGlobalRegistry()
	reg.RegisterClient(id, "claude", []*registry.ModelInfo{{ID: availableModel}, {ID: failedModel}})
	t.Cleanup(func() { reg.UnregisterClient(id) })
	if _, err := manager.Register(ctx, &Auth{ID: id, Provider: "claude"}); err != nil {
		t.Fatal(err)
	}
	retry := 7 * 24 * time.Hour
	manager.MarkResult(ctx, Result{AuthID: id, Provider: "claude", Model: availableModel, CredentialScope: true, RetryAfter: &retry, Error: &Error{HTTPStatus: http.StatusTooManyRequests}})
	manager.MarkResult(ctx, Result{AuthID: id, Provider: "claude", Model: failedModel, Error: &Error{HTTPStatus: http.StatusBadRequest, Message: "unsupported model"}})
	manager.MarkResult(ctx, Result{AuthID: id, Provider: "claude", Model: availableModel, CredentialScope: true, RetryAfter: &retry, Error: &Error{HTTPStatus: http.StatusTooManyRequests}})
	if len(store.savedRecords()) < 2 {
		t.Fatal("quota cooldown was not persisted before recovery")
	}
	observation, _ := manager.GetByID(id)
	updated, _, err := manager.resetQuota(ctx, id, observation)
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil || updated.ModelStates[availableModel].Unavailable || !updated.ModelStates[failedModel].Unavailable {
		t.Fatal("recovery did not preserve the unrelated model failure")
	}
	if updated.ModelStates[failedModel].LastError == nil || updated.ModelStates[failedModel].LastError.HTTPStatus != http.StatusBadRequest {
		t.Fatal("recovery erased the model rejection")
	}
	records := store.savedRecords()
	if len(records) != 1 {
		t.Fatalf("persisted cooldowns after recovery = %d, want only the unrelated failure", len(records))
	}
	for _, record := range records {
		if record.Model == availableModel || record.Model == "" {
			t.Fatalf("recovered quota persisted a stale cooldown: %+v", record)
		}
	}
	if reg.GetModelCount(availableModel) != 1 || reg.GetModelCount(failedModel) != 0 {
		t.Fatal("registry projection did not preserve per-model availability")
	}
}
