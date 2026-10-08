package config

import "github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"

type ClientProfile = clientprofiles.Profile
type ClientProfilePolicy = clientprofiles.Policy
type ClientProfileKey = clientprofiles.Key

func (cfg *Config) ValidateClientProfiles() error {
	if (len(cfg.ClientProfileKeys) > 0 || len(cfg.RevokedClientProfileKeys) > 0) && !cfg.WebsocketAuth {
		return clientprofiles.Invalid("websocket_auth_required", "oauth.providers.aistudio.ws-auth")
	}
	if err := clientprofiles.ValidateRevocations(cfg.ClientProfileKeys, cfg.RevokedClientProfileKeys); err != nil {
		return err
	}
	return clientprofiles.Validate(cfg.ClientProfiles, cfg.ClientProfileKeys, cfg.APIKeys)
}
