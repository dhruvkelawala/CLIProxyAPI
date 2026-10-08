package configaccess

import (
	"context"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v8/sdk/access"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

type credentialOwner struct {
	inventory *provider
	revoked   map[string]bool
	invalid   bool
}

// NewCredentialOwner captures configured identities independently of provider precedence.
func NewCredentialOwner(cfg *sdkconfig.SDKConfig) sdkaccess.CredentialOwner {
	o := &credentialOwner{inventory: configuredProvider(cfg), revoked: make(map[string]bool)}
	o.invalid = clientprofiles.Validate(o.inventory.profiles, o.inventory.bindings, cfg.APIKeys) != nil
	o.invalid = o.invalid || clientprofiles.ValidateRevocations(cfg.ClientProfileKeys, cfg.RevokedClientProfileKeys) != nil
	for _, fingerprint := range cfg.RevokedClientProfileKeys {
		o.revoked[fingerprint] = true
	}

	return o
}

func (o *credentialOwner) Bind(ctx context.Context, r *http.Request, accepted *sdkaccess.Result) (*sdkaccess.Result, *sdkaccess.AuthError) {
	if o.invalid {
		return nil, &sdkaccess.AuthError{Code: "client_profile_invalid", Message: "Client profile is unavailable", StatusCode: 503}
	}
	copyResult := sdkaccess.Result{}
	if accepted != nil {
		copyResult = *accepted
	}
	copyResult.Metadata = make(map[string]string)
	if accepted != nil {
		for key, value := range accepted.Metadata {
			if key != clientprofiles.BindingMetadataKey {
				copyResult.Metadata[key] = value
			}
		}
	}
	credentials := make(map[string]bool)
	var bindingRelevant bool
	for _, raw := range requestCredentials(r) {
		if raw == "" {
			continue
		}
		credentials[raw] = true
		fingerprint := clientprofiles.Fingerprint(raw)
		if o.revoked[fingerprint] {
			return nil, sdkaccess.NewInvalidCredentialError()
		}
		_, configured := o.inventory.keys[raw]
		if !configured {
			continue
		}
		snapshot, err := clientprofiles.Binding(o.inventory.profiles, o.inventory.bindings, raw)
		if err != nil {
			return nil, &sdkaccess.AuthError{Code: "client_profile_invalid", Message: "Client profile is unavailable", StatusCode: 503}
		}
		bindingRelevant = bindingRelevant || snapshot.Bound
	}
	if bindingRelevant {
		if len(credentials) != 1 {
			return nil, sdkaccess.NewInvalidCredentialError()
		}
		native, err := o.inventory.Authenticate(ctx, r)
		if err != nil {
			return nil, err
		}
		return native, nil
	}
	if accepted != nil {
		if accepted.Provider == o.inventory.Identifier() {
			if _, exists := o.inventory.keys[accepted.Principal]; !exists {
				return nil, sdkaccess.NewInvalidCredentialError()
			}
		}
		if o.revoked[clientprofiles.Fingerprint(accepted.Principal)] {
			return nil, sdkaccess.NewInvalidCredentialError()
		}
		claimed, err := clientprofiles.Binding(o.inventory.profiles, o.inventory.bindings, accepted.Principal)
		if err != nil || claimed.Bound {
			return nil, sdkaccess.NewInvalidCredentialError()
		}
	}
	if accepted == nil {
		return nil, nil
	}
	return &copyResult, nil
}

func requestCredentials(r *http.Request) []string {
	if r == nil {
		return nil
	}
	values := []string{}
	for _, value := range r.Header.Values("Authorization") {
		values = append(values, extractBearerToken(value))
	}
	for _, header := range []string{"X-Goog-Api-Key", "X-Api-Key"} {
		values = append(values, r.Header.Values(header)...)
	}
	if r.URL != nil {
		values = append(values, r.URL.Query()["key"]...)
		values = append(values, r.URL.Query()["auth_token"]...)
	}
	return values
}
