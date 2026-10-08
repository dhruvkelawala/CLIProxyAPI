package clientprofiles

import (
	"context"
	"encoding/json"
	"reflect"
)

type bindingContextKey struct{}

// WithBinding captures the authenticated association independently of caller headers.
func WithBinding(ctx context.Context, snapshot Snapshot) context.Context {
	snapshot.Policies = clonePolicies(snapshot.Policies)
	return context.WithValue(ctx, bindingContextKey{}, snapshot)
}
func clonePolicies(policies map[string]Policy) map[string]Policy {
	out := make(map[string]Policy, len(policies))
	for provider, policy := range policies {
		out[provider] = policy
	}
	return out
}
func FromContext(ctx context.Context) Snapshot {
	if ctx == nil {
		return Snapshot{}
	}
	snapshot, _ := ctx.Value(bindingContextKey{}).(Snapshot)
	snapshot.Policies = clonePolicies(snapshot.Policies)
	return snapshot
}
func Capture(ctx context.Context, metadata map[string]string) (context.Context, error) {
	encoded, exists := metadata[BindingMetadataKey]
	if !exists {
		return ctx, nil
	}
	var snapshot Snapshot
	if json.Unmarshal([]byte(encoded), &snapshot) != nil || !snapshot.Bound {
		return ctx, Invalid("client_profile_invalid", "binding")
	}
	return WithBinding(ctx, snapshot), nil
}
func Strict(ctx context.Context) bool {
	snapshot := FromContext(ctx)
	if !snapshot.Bound {
		return false
	}
	for _, policy := range snapshot.Policies {
		if policy.Mode == "only" {
			return true
		}
	}
	return false
}
func ValidateBinding(snapshot Snapshot, profiles []Profile, keys []Key, apiKeys []string) error {
	if !snapshot.Bound {
		return nil
	}
	for _, key := range keys {
		if key.Ref != snapshot.KeyRef {
			continue
		}
		accepted := false
		for _, raw := range apiKeys {
			if Fingerprint(raw) == key.Fingerprint {
				accepted = true
				break
			}
		}
		if !accepted || key.Fingerprint != snapshot.KeyFingerprint || key.Revision != snapshot.KeyRevision || key.ProfileRef != snapshot.ProfileRef {
			break
		}
		for _, profile := range profiles {
			if profile.Ref == snapshot.ProfileRef && profile.Revision == snapshot.ProfileRevision && reflect.DeepEqual(profile.Policies, snapshot.Policies) {
				return Validate([]Profile{profile}, nil, nil)
			}
		}
		break
	}
	return Invalid("client_profile_stale", "binding")
}

type ExecutionError struct {
	Code  string `json:"code"`
	Field string `json:"field,omitempty"`
}

func (e *ExecutionError) Error() string {
	body, _ := json.Marshal(struct {
		Error *ExecutionError `json:"error"`
	}{e})
	return string(body)
}
func (*ExecutionError) StatusCode() int { return 503 }
func Denied(code, field string) error   { return &ExecutionError{Code: code, Field: field} }
