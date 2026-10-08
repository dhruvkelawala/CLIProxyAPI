// Package clientprofiles owns persisted client identities and provider policies.
package clientprofiles

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const AccountRefMetadataKey = "account_ref"
const BindingMetadataKey = "client_profile_binding"

func Providers() []string { return []string{"claude", "codex"} }

type Policy struct {
	Mode       string `json:"mode" yaml:"mode"`
	AccountRef string `json:"account_ref,omitempty" yaml:"account_ref,omitempty"`
}

type Profile struct {
	Ref      string            `json:"profile_ref" yaml:"profile_ref"`
	Label    string            `json:"label" yaml:"label"`
	Revision uint64            `json:"revision" yaml:"revision"`
	Policies map[string]Policy `json:"policies" yaml:"policies"`
}

type Key struct {
	Ref         string `json:"key_ref" yaml:"key_ref"`
	Label       string `json:"label" yaml:"label"`
	ProfileRef  string `json:"profile_ref" yaml:"profile_ref"`
	Fingerprint string `json:"-" yaml:"fingerprint"`
	Revision    uint64 `json:"revision" yaml:"revision"`
}

type Account struct {
	CredentialRef       string `json:"credential_ref"`
	AccountRef          string `json:"account_ref,omitempty"`
	Provider            string `json:"provider"`
	Label               string `json:"label"`
	Available           bool   `json:"available"`
	State               string `json:"state"`
	EnrollmentSupported bool   `json:"enrollment_supported"`
	TargetSupported     bool   `json:"target_supported"`
}

type Error struct {
	Code  string `json:"code"`
	Field string `json:"field,omitempty"`
}

func (e *Error) Error() string         { return e.Code + ": " + e.Field }
func Invalid(code, field string) error { return &Error{Code: code, Field: field} }
func Fingerprint(key string) string {
	s := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(s[:])
}
func CredentialRef(id string) string { return "credential_" + Fingerprint(id) }
func ValidRef(ref string) bool {
	u, e := uuid.Parse(ref)
	return e == nil && u != uuid.Nil && u.String() == ref
}
func Supported(provider string) bool {
	for _, p := range Providers() {
		if provider == p {
			return true
		}
	}
	return false
}

func Validate(profiles []Profile, keys []Key, apiKeys []string) error {
	refs := map[string]bool{}
	labels := map[string]bool{}
	for _, p := range profiles {
		if !ValidRef(p.Ref) || refs[p.Ref] {
			return Invalid("invalid_profile_ref", p.Ref)
		}
		refs[p.Ref] = true
		if strings.TrimSpace(p.Label) == "" || labels[p.Label] {
			return Invalid("invalid_label", p.Ref)
		}
		labels[p.Label] = true
		if len(p.Policies) != len(Providers()) {
			return Invalid("missing_provider_policy", p.Ref)
		}
		for provider, policy := range p.Policies {
			if !Supported(provider) {
				return Invalid("unknown_provider", provider)
			}
			switch policy.Mode {
			case "automatic":
				if policy.AccountRef != "" {
					return Invalid("unexpected_target", provider)
				}
			case "only":
				if !ValidRef(policy.AccountRef) {
					return Invalid("invalid_account_ref", provider)
				}
			default:
				return Invalid("unknown_mode", provider)
			}
		}
	}
	seenRefs, seenKeys := map[string]bool{}, map[string]bool{}
	knownKeys := map[string]bool{}
	for _, key := range apiKeys {
		knownKeys[Fingerprint(key)] = true
	}
	for _, key := range keys {
		if !ValidRef(key.Ref) || seenRefs[key.Ref] {
			return Invalid("invalid_key_ref", key.Ref)
		}
		seenRefs[key.Ref] = true
		if !refs[key.ProfileRef] {
			return Invalid("unknown_profile", key.ProfileRef)
		}
		raw, e := hex.DecodeString(key.Fingerprint)
		if e != nil || len(raw) != 32 || seenKeys[key.Fingerprint] {
			return Invalid("invalid_key_association", key.Ref)
		}
		seenKeys[key.Fingerprint] = true
		if !knownKeys[key.Fingerprint] {
			return Invalid("key_removed", key.Ref)
		}
		if strings.TrimSpace(key.Label) == "" {
			return Invalid("invalid_label", key.Ref)
		}
	}
	return nil
}

// Resolve checks current inventory, counting disabled duplicates as ambiguous.
func Resolve(provider string, policy Policy, accounts []Account) string {
	if !Supported(provider) {
		return "unknown_provider"
	}
	if policy.Mode == "automatic" && policy.AccountRef == "" {
		return "automatic"
	}
	if policy.Mode != "only" {
		return "unknown_mode"
	}
	if !ValidRef(policy.AccountRef) {
		return "invalid_account_ref"
	}
	var matches []Account
	for _, a := range accounts {
		if a.AccountRef == policy.AccountRef {
			matches = append(matches, a)
		}
	}
	if len(matches) == 0 {
		return "target_removed"
	}
	if len(matches) > 1 {
		return "duplicate_account_ref"
	}
	if matches[0].Provider != provider {
		return "provider_mismatch"
	}
	if !matches[0].TargetSupported {
		return "target_unsupported"
	}
	if !matches[0].Available {
		return "target_unavailable"
	}
	return "available"
}
func ValidateTargets(p Profile, accounts []Account) error {
	for provider, policy := range p.Policies {
		if policy.Mode == "only" {
			state := Resolve(provider, policy, accounts)
			if state != "available" {
				return Invalid(state, fmt.Sprintf("policies.%s", provider))
			}
		}
	}
	return nil
}

type Snapshot struct {
	Bound           bool              `json:"bound"`
	ProfileRef      string            `json:"profile_ref,omitempty"`
	KeyRef          string            `json:"key_ref,omitempty"`
	ProfileRevision uint64            `json:"profile_revision,omitempty"`
	KeyFingerprint  string            `json:"key_fingerprint,omitempty"`
	KeyRevision     uint64            `json:"key_revision,omitempty"`
	Policies        map[string]Policy `json:"policies,omitempty"`
}

// Binding resolves an accepted key without treating a broken association as legacy.
func Binding(profiles []Profile, keys []Key, principal string) (Snapshot, error) {
	fingerprint := Fingerprint(principal)
	for _, key := range keys {
		if key.Fingerprint == fingerprint {
			snapshot := Snapshot{Bound: true, KeyRef: key.Ref, KeyFingerprint: fingerprint, KeyRevision: key.Revision, ProfileRef: key.ProfileRef}
			for _, p := range profiles {
				if p.Ref == key.ProfileRef {
					snapshot.ProfileRevision = p.Revision
					snapshot.Policies = make(map[string]Policy, len(p.Policies))
					for provider, policy := range p.Policies {
						snapshot.Policies[provider] = policy
					}
					if err := Validate([]Profile{p}, nil, nil); err != nil {
						return snapshot, err
					}
					return snapshot, nil
				}
			}
			return snapshot, Invalid("unknown_profile", key.ProfileRef)
		}
	}
	return Snapshot{}, nil
}

// ValidateRevocations validates persisted profile-key revocation and active-binding exclusion.
func ValidateRevocations(keys []Key, revoked []string) error {
	seen := make(map[string]bool)
	for _, fingerprint := range revoked {
		raw, err := hex.DecodeString(fingerprint)
		if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != fingerprint || seen[fingerprint] {
			return Invalid("invalid_revoked_key", "access.revoked-client-profile-keys")
		}
		seen[fingerprint] = true
	}
	for _, key := range keys {
		if seen[key.Fingerprint] {
			return Invalid("revoked_key_bound", "access.client-profile-keys")
		}
	}

	return nil
}
