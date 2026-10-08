package access

import (
	"context"
	"net/http"
	"sync"
)

// Manager coordinates authentication providers.
type Manager struct {
	mu                      sync.RWMutex
	providers               []Provider
	credentialOwner         CredentialOwner
	credentialOwnerRequired bool
}

// NewManager constructs an empty manager.
func NewManager() *Manager {
	return &Manager{}
}

// SetProviders replaces the active provider list.
func (m *Manager) SetProviders(providers []Provider) {
	if m == nil {
		return
	}
	cloned := make([]Provider, len(providers))
	copy(cloned, providers)
	m.mu.Lock()
	m.providers = cloned
	m.mu.Unlock()
}

// Providers returns a snapshot of the active providers.
func (m *Manager) Providers() []Provider {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	snapshot := make([]Provider, len(m.providers))
	copy(snapshot, m.providers)
	return snapshot
}

// Authenticate evaluates providers until one succeeds.
func (m *Manager) Authenticate(ctx context.Context, r *http.Request) (*Result, *AuthError) {
	if m == nil {
		return nil, nil
	}
	providers := m.Providers()
	if len(providers) == 0 {
		return m.BindAcceptedCredential(ctx, r, nil)
	}

	var (
		missing bool
		invalid bool
	)

	for _, provider := range providers {
		if provider == nil {
			continue
		}
		res, authErr := provider.Authenticate(ctx, r)
		if authErr == nil {
			return m.BindAcceptedCredential(ctx, r, res)
		}
		if IsAuthErrorCode(authErr, AuthErrorCodeNotHandled) {
			continue
		}
		if IsAuthErrorCode(authErr, AuthErrorCodeNoCredentials) {
			missing = true
			continue
		}
		if IsAuthErrorCode(authErr, AuthErrorCodeInvalidCredential) {
			invalid = true
			continue
		}
		return nil, authErr
	}

	if invalid {
		return nil, NewInvalidCredentialError()
	}
	if missing {
		return nil, NewNoCredentialsError()
	}
	return nil, NewNoCredentialsError()
}

// CredentialOwner applies configured credential ownership after provider authentication.
type CredentialOwner interface {
	Bind(context.Context, *http.Request, *Result) (*Result, *AuthError)
}

// SetProvidersAndCredentialOwner publishes providers and their credential owner together.
func (m *Manager) SetProvidersAndCredentialOwner(providers []Provider, owner CredentialOwner) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers = append([]Provider(nil), providers...)
	m.credentialOwner = owner
	if owner != nil {
		m.credentialOwnerRequired = true
	}
}

// CredentialOwnerAvailable reports whether the configured owner remains installed.
func (m *Manager) CredentialOwnerAvailable() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.credentialOwner != nil
}

// BindAcceptedCredential validates configured ownership without rerunning frontend authentication.
func (m *Manager) BindAcceptedCredential(ctx context.Context, r *http.Request, accepted *Result) (*Result, *AuthError) {
	if m == nil {
		return accepted, nil
	}
	m.mu.RLock()
	owner := m.credentialOwner
	required := m.credentialOwnerRequired
	m.mu.RUnlock()
	if owner == nil {
		if required {
			return nil, &AuthError{Code: "profile_owner_unavailable", Message: "Configured credential owner is unavailable", StatusCode: 503}
		}
		return accepted, nil
	}
	return owner.Bind(ctx, r, accepted)
}
