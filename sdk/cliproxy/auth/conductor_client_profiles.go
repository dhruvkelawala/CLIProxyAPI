package auth

import (
	"context"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

type profileTargetContextKey struct{}
type profileModelContextKey struct{}

func profileExecutionError(code, field string) error {
	return wrapRequestStopError(clientprofiles.Denied(code, field))
}

// ValidateClientProfile rejects obsolete accepted associations, including Automatic sessions.
func (m *Manager) ValidateClientProfile(ctx context.Context) error {
	snapshot := clientprofiles.FromContext(ctx)
	if !snapshot.Bound {
		return nil
	}
	if m == nil {
		return profileExecutionError("profile_owner_unavailable", "binding")
	}
	cfg := m.runtimeConfigSnapshot()
	if cfg == nil {
		return profileExecutionError("profile_owner_unavailable", "binding")
	}
	if err := clientprofiles.ValidateBinding(snapshot, cfg.ClientProfiles, cfg.ClientProfileKeys, cfg.APIKeys); err != nil {
		return profileExecutionError("client_profile_stale", "binding")
	}
	return nil
}

// ClientProfileAccounts reports the current inventory used by preview and enforcement.
func (m *Manager) ClientProfileAccounts() []clientprofiles.Account {
	inventory := make([]clientprofiles.Account, 0)
	if m == nil {
		return inventory
	}
	for _, a := range m.List() {
		ref, _ := a.Metadata[clientprofiles.AccountRefMetadataKey].(string)
		available := !a.Disabled && !a.Unavailable && a.Status != StatusDisabled
		enrollment := m.SupportsAccountEnrollment(a.ID)
		supported := enrollment && executorKeyFromAuth(a) == a.Provider
		label := a.Label
		if strings.TrimSpace(label) == "" {
			label = a.Provider + " credential"
		}
		state := "available"
		if ref == "" {
			state = "not_enrolled"
		}
		if !available {
			state = "unavailable"
		}
		inventory = append(inventory, clientprofiles.Account{CredentialRef: clientprofiles.CredentialRef(a.ID), AccountRef: ref, Provider: a.Provider, Label: label, Available: available, State: state, EnrollmentSupported: enrollment, TargetSupported: supported})
	}
	for i := range inventory {
		if inventory[i].AccountRef != "" {
			state := clientprofiles.Resolve(inventory[i].Provider, clientprofiles.Policy{Mode: "only", AccountRef: inventory[i].AccountRef}, inventory)
			if state != "available" {
				inventory[i].State = state
				inventory[i].Available = false
			}
		}
	}
	return inventory
}

func (m *Manager) constrainClientProfile(ctx context.Context, providers []string, model string, opts cliproxyexecutor.Options) (context.Context, cliproxyexecutor.Options, error) {
	if err := m.ValidateClientProfile(ctx); err != nil {
		return ctx, opts, err
	}
	if clientprofiles.FromContext(ctx).Bound && cliproxyexecutor.WebsocketInputFromContext(ctx) != nil {
		return ctx, opts, profileExecutionError("profile_protocol_unsupported", "bound_duplex")
	}
	if !clientprofiles.Strict(ctx) {
		return ctx, opts, nil
	}
	if m.HomeEnabled() {
		return ctx, opts, profileExecutionError("profile_home_unsupported", "home")
	}
	if len(providers) != 1 {
		return ctx, opts, profileExecutionError("profile_mixed_provider_unsupported", "providers")
	}
	policy, exists := clientprofiles.FromContext(ctx).Policies[providers[0]]
	if !exists {
		return ctx, opts, profileExecutionError("profile_provider_unsupported", providers[0])
	}
	if policy.Mode == "automatic" {
		return ctx, opts, nil
	}
	inventory := m.ClientProfileAccounts()
	if state := clientprofiles.Resolve(providers[0], policy, inventory); state != "available" {
		return ctx, opts, profileExecutionError(state, providers[0])
	}
	target := ""
	for _, auth := range m.List() {
		ref, _ := auth.Metadata[clientprofiles.AccountRefMetadataKey].(string)
		if ref == policy.AccountRef {
			target = auth.ID
			break
		}
	}
	if target == "" {
		return ctx, opts, profileExecutionError("target_removed", providers[0])
	}
	if pin := pinnedAuthIDFromMetadata(opts.Metadata); pin != "" && pin != target {
		return ctx, opts, profileExecutionError("profile_pin_conflict", "auth")
	}
	metadata := make(map[string]any, len(opts.Metadata)+1)
	for key, value := range opts.Metadata {
		metadata[key] = value
	}
	metadata[cliproxyexecutor.PinnedAuthMetadataKey] = target
	opts.Metadata = metadata
	ctx = context.WithValue(ctx, profileModelContextKey{}, model)
	return context.WithValue(ctx, profileTargetContextKey{}, target), opts, nil
}
func (m *Manager) validateProfileExecution(ctx context.Context, a *Auth, executor ProviderExecutor, opts cliproxyexecutor.Options) error {
	if err := m.ValidateClientProfile(ctx); err != nil {
		return err
	}
	if !clientprofiles.Strict(ctx) {
		return nil
	}
	if a == nil || executor == nil {
		return profileExecutionError("target_removed", "auth")
	}
	snapshot := clientprofiles.FromContext(ctx)
	provider := strings.ToLower(strings.TrimSpace(a.Provider))
	policy, exists := snapshot.Policies[provider]
	if !exists {
		return profileExecutionError("profile_provider_unsupported", provider)
	}
	if IsPluginVirtualAuth(a) || executorKeyFromAuth(a) != provider || executor.Identifier() != provider {
		return profileExecutionError("target_unsupported", provider)
	}
	if policy.Mode == "automatic" {
		return nil
	}
	if state := clientprofiles.Resolve(provider, policy, m.ClientProfileAccounts()); state != "available" {
		return profileExecutionError(state, provider)
	}
	current, exists := m.GetByID(a.ID)
	if !exists || current == nil {
		return profileExecutionError("target_removed", provider)
	}
	if current.Disabled || current.Unavailable || current.Status == StatusDisabled {
		return profileExecutionError("target_unavailable", provider)
	}
	model, _ := ctx.Value(profileModelContextKey{}).(string)
	if blocked, _, _ := isAuthBlockedForModel(current, m.selectionModelKeyForAuth(current, model), time.Now()); blocked {
		return profileExecutionError("target_unavailable", provider)
	}
	ref, _ := current.Metadata[clientprofiles.AccountRefMetadataKey].(string)
	executionRef, _ := a.Metadata[clientprofiles.AccountRefMetadataKey].(string)
	target, _ := ctx.Value(profileTargetContextKey{}).(string)
	if ref != policy.AccountRef || executionRef != ref || a.ID != target || current.Provider != a.Provider || executorKeyFromAuth(current) != provider {
		return profileExecutionError("profile_target_changed", provider)
	}
	if pin := pinnedAuthIDFromMetadata(opts.Metadata); pin != "" && pin != a.ID {
		return profileExecutionError("profile_pin_conflict", "auth")
	}
	return nil
}

func validateProfileReplacement(ctx context.Context, base, updated *Auth) error {
	if !clientprofiles.Strict(ctx) || updated == nil {
		return nil
	}
	baseRef, _ := base.Metadata[clientprofiles.AccountRefMetadataKey].(string)
	updatedRef, _ := updated.Metadata[clientprofiles.AccountRefMetadataKey].(string)
	if (updated.ID != "" && updated.ID != base.ID) || (updated.Provider != "" && updated.Provider != base.Provider) || (updatedRef != "" && updatedRef != baseRef) {
		return profileExecutionError("profile_target_changed", "credential_acquisition")
	}
	return nil
}
