package auth_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	claude "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/claude"
	codex "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/codex"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/watcher/synthesizer"
	sdkauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestClientProfileFileEnrollmentRefreshRename(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			dir := t.TempDir()
			store := sdkauth.NewFileTokenStore()
			store.SetBaseDir(dir)
			manager := coreauth.NewManager(store, nil, nil)
			metadata := map[string]any{"type": provider, "label": "Synthetic account", "access_token": "synthetic-old"}
			a := &coreauth.Auth{ID: "before.json", FileName: "before.json", Provider: provider, Metadata: metadata, Status: coreauth.StatusActive}
			if provider == "claude" {
				a.Storage = &claude.ClaudeTokenStorage{AccessToken: "synthetic-old", Metadata: metadata}
			} else {
				a.Storage = &codex.CodexTokenStorage{AccessToken: "synthetic-old", Metadata: metadata}
			}
			registered, err := manager.Register(context.Background(), a)
			if err != nil {
				t.Fatal(err)
			}
			originalStorage := registered.Storage
			enrolled, err := manager.EnrollAccountReference(context.Background(), a.ID)
			if err != nil {
				t.Fatal(err)
			}
			ref := enrolled.Metadata[clientprofiles.AccountRefMetadataKey].(string)
			if !clientprofiles.ValidRef(ref) {
				t.Fatal("bad UUID")
			}
			if enrolled.Storage == originalStorage {
				t.Fatal("enrollment shared token storage")
			}
			if _, err = manager.EnrollAccountReference(context.Background(), a.ID); err == nil {
				t.Fatal("reenrollment accepted")
			}
			refreshed := enrolled.Clone()
			refreshed.Metadata = map[string]any{"type": provider, "access_token": "synthetic-new", clientprofiles.AccountRefMetadataKey: "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"}
			refreshed.Storage = nil
			if _, err = manager.UpdateRefreshedAuth(context.Background(), enrolled, refreshed); err != nil {
				t.Fatal(err)
			}
			files, err := store.List(context.Background())
			if err != nil || len(files) != 1 {
				t.Fatal(files, err)
			}
			if files[0].Metadata[clientprofiles.AccountRefMetadataKey] != ref {
				t.Fatal("refresh/store lost reference")
			}
			if err = os.Rename(filepath.Join(dir, "before.json"), filepath.Join(dir, "renamed.json")); err != nil {
				t.Fatal(err)
			}
			ctx := &synthesizer.SynthesisContext{Config: &config.Config{}, AuthDir: dir, IDGenerator: synthesizer.NewStableIDGenerator()}
			auths, err := synthesizer.NewFileSynthesizer().Synthesize(ctx)
			if err != nil || len(auths) != 1 {
				t.Fatal(auths, err)
			}
			if auths[0].Metadata[clientprofiles.AccountRefMetadataKey] != ref {
				t.Fatal("watcher rename lost reference")
			}
			reloaded := coreauth.NewManager(store, nil, nil)
			if err = reloaded.Load(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(reloaded.List()) != 1 || reloaded.List()[0].Metadata[clientprofiles.AccountRefMetadataKey] != ref {
				t.Fatal("reload lost reference")
			}
			raw, err := os.ReadFile(filepath.Join(dir, "renamed.json"))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "copied.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			auths, err = synthesizer.NewFileSynthesizer().Synthesize(ctx)
			if err != nil || len(auths) != 2 {
				t.Fatal(auths, err)
			}
			accounts := []clientprofiles.Account{}
			for _, entry := range auths {
				accounts = append(accounts, clientprofiles.Account{Provider: provider, AccountRef: entry.Metadata[clientprofiles.AccountRefMetadataKey].(string), Available: true, TargetSupported: true})
			}
			if clientprofiles.Resolve(provider, clientprofiles.Policy{Mode: "only", AccountRef: ref}, accounts) != "duplicate_account_ref" {
				t.Fatal("copied file silently resolved")
			}
		})
	}
}
func TestClientProfileFileEnrollmentFailureLeavesStorageAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "synthetic.json")
	before := []byte(`{"type":"claude","access_token":"synthetic-old"}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	metadata := map[string]any{"type": "claude", "bad": make(chan int)}
	storage := &claude.ClaudeTokenStorage{AccessToken: "synthetic-old", Metadata: map[string]any{"type": "claude"}}
	initialStorage := map[string]any{"type": "claude"}
	store := sdkauth.NewFileTokenStore()
	store.SetBaseDir(dir)
	manager := coreauth.NewManager(store, nil, nil)
	a, err := manager.Register(coreauth.WithSkipPersist(context.Background()), &coreauth.Auth{ID: "synthetic.json", FileName: "synthetic.json", Provider: "claude", Metadata: metadata, Storage: storage})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.EnrollAccountReference(context.Background(), a.ID); err == nil {
		t.Fatal("bad metadata enrollment succeeded")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("failed enrollment damaged file", err)
	}
	if !reflect.DeepEqual(storage.Metadata, initialStorage) {
		t.Fatal("failed enrollment mutated shared storage")
	}
	current, _ := manager.GetByID(a.ID)
	if _, ok := current.Metadata[clientprofiles.AccountRefMetadataKey]; ok {
		t.Fatal("failed enrollment published metadata")
	}
	refreshed := current.Clone()
	refreshed.Metadata = map[string]any{"type": "claude", "access_token": "synthetic-new"}
	recovered, err := manager.UpdateRefreshedAuth(coreauth.WithSkipPersist(context.Background()), current, refreshed)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := recovered.Metadata[clientprofiles.AccountRefMetadataKey]; ok {
		t.Fatal("later refresh gained failed enrollment reference")
	}
	if _, ok := storage.Metadata[clientprofiles.AccountRefMetadataKey]; ok {
		t.Fatal("later storage refresh gained failed enrollment reference")
	}
	files, err := store.List(context.Background())
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	raw, _ := json.Marshal(files[0].Metadata)
	if string(raw) == "" {
		t.Fatal("empty loaded metadata")
	}
}
