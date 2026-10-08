package management

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/clientprofiles"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"gopkg.in/yaml.v3"
)

func profileError(c *gin.Context, status int, err error) {
	c.Header("Cache-Control", "no-store")
	var domain *clientprofiles.Error
	if errors.As(err, &domain) {
		c.JSON(status, gin.H{"error": domain})
		return
	}
	c.JSON(status, gin.H{"error": gin.H{"code": "persistence_failed"}})
}
func profileBody(c *gin.Context, dst any) bool {
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		profileError(c, 400, clientprofiles.Invalid("invalid_body", ""))
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		profileError(c, 400, clientprofiles.Invalid("invalid_body", ""))
		return false
	}
	return true
}
func (h *Handler) profileAccountsLocked() []clientprofiles.Account {
	result := make([]clientprofiles.Account, 0)
	if h.authManager == nil {
		return result
	}
	for _, a := range h.authManager.List() {
		if a == nil {
			continue
		}
		ref, _ := a.Metadata[clientprofiles.AccountRefMetadataKey].(string)
		available := !a.Disabled && !a.Unavailable && string(a.Status) != "disabled"
		state := "available"
		if ref == "" {
			state = "not_enrolled"
		}
		if !available {
			state = "unavailable"
		}
		label := a.Label
		if strings.TrimSpace(label) == "" {
			label = a.Provider + " credential"
		}
		result = append(result, clientprofiles.Account{CredentialRef: clientprofiles.CredentialRef(a.ID), AccountRef: ref, Provider: a.Provider, Label: label, Available: available, State: state, EnrollmentSupported: h.authManager.SupportsAccountEnrollment(a.ID), TargetSupported: h.authManager.SupportsAccountEnrollment(a.ID)})
	}
	for i := range result {
		if result[i].AccountRef != "" {
			state := clientprofiles.Resolve(result[i].Provider, clientprofiles.Policy{Mode: "only", AccountRef: result[i].AccountRef}, result)
			if state != "available" {
				result[i].State = state
				result[i].Available = false
			}
		}
	}
	return result
}
func (h *Handler) profileRevisionLocked(c *gin.Context, mutation bool) ([]byte, string, bool) {
	raw, err := os.ReadFile(h.configFilePath)
	if err != nil {
		profileError(c, 500, err)
		return nil, "", false
	}
	persisted, err := config.ParseConfigBytes(raw)
	if err != nil {
		profileError(c, 503, clientprofiles.Invalid("invalid_persisted_config", "config"))
		return nil, "", false
	}
	if !sameProfileSlice(persisted.ClientProfiles, h.cfg.ClientProfiles) || !sameProfileSlice(persisted.ClientProfileKeys, h.cfg.ClientProfileKeys) || !sameProfileSlice(persisted.APIKeys, h.cfg.APIKeys) || !sameProfileSlice(persisted.RevokedClientProfileKeys, h.cfg.RevokedClientProfileKeys) {
		profileError(c, 409, clientprofiles.Invalid("reload_pending", "config"))
		return nil, "", false
	}
	revision := `"` + clientprofiles.Fingerprint(string(raw)) + `"`
	c.Header("ETag", revision)
	c.Header("Cache-Control", "no-store")
	if mutation && c.GetHeader("If-Match") != revision {
		code, status := "stale_revision", 412
		if c.GetHeader("If-Match") == "" {
			code, status = "revision_required", 428
		}
		profileError(c, status, clientprofiles.Invalid(code, "If-Match"))
		return nil, "", false
	}
	return raw, revision, true
}

// ClientProfileCapabilities describes the persisted contract, not active routing enforcement.
func (h *Handler) ClientProfileCapabilities(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"contract_version": 1, "management": true, "enforcement": false, "strict_requests": "rejected", "providers": clientprofiles.Providers(), "modes": []string{"automatic", "only"}, "session_behavior": "fresh_session_required", "binding_requires": []string{"websocket_auth_enabled"}, "enrollment_stores": []string{"file"}, "enrollment_storage": []string{"metadata", "native_claude", "native_codex"}, "unsupported": []string{"home_strict", "mixed_provider_strict", "plugin_virtual_credentials", "unenrolled_credentials", "non_file_enrollment", "prefer", "fallback"}})
}
func (h *Handler) ClientProfiles(c *gin.Context) {
	h.mu.Lock()
	raw, revision, ok := h.profileRevisionLocked(c, c.Request.Method != http.MethodGet)
	if !ok {
		h.mu.Unlock()
		return
	}
	if c.Request.Method == http.MethodGet {
		accounts := h.profileAccountsLocked()
		profiles := h.cfg.ClientProfiles
		if profiles == nil {
			profiles = []clientprofiles.Profile{}
		}
		states := map[string]map[string]string{}
		for _, p := range profiles {
			states[p.Ref] = map[string]string{}
			for provider, policy := range p.Policies {
				states[p.Ref][provider] = clientprofiles.Resolve(provider, policy, accounts)
			}
		}
		c.JSON(200, gin.H{"revision": revision, "profiles": profiles, "keys": h.cfg.ClientProfileKeys, "accounts": accounts, "target_states": states})
		h.mu.Unlock()
		return
	}
	next := h.cfg.CloneForRuntime()
	var result any
	ref := c.Param("ref")
	switch c.Request.Method {
	case http.MethodPost:
		var body struct {
			Label    string                           `json:"label"`
			Policies map[string]clientprofiles.Policy `json:"policies"`
		}
		if !profileBody(c, &body) {
			h.mu.Unlock()
			return
		}
		p := clientprofiles.Profile{Ref: uuid.NewString(), Label: body.Label, Revision: 1, Policies: body.Policies}
		if err := clientprofiles.ValidateTargets(p, h.profileAccountsLocked()); err != nil {
			profileError(c, 422, err)
			h.mu.Unlock()
			return
		}
		next.ClientProfiles = append(next.ClientProfiles, p)
		result = p
	case http.MethodPut, http.MethodDelete:
		index := -1
		for i, p := range next.ClientProfiles {
			if p.Ref == ref {
				index = i
				break
			}
		}
		if index < 0 {
			profileError(c, 404, clientprofiles.Invalid("profile_not_found", ref))
			h.mu.Unlock()
			return
		}
		if c.Request.Method == http.MethodDelete {
			for _, key := range next.ClientProfileKeys {
				if key.ProfileRef == ref {
					profileError(c, 409, clientprofiles.Invalid("profile_in_use", ref))
					h.mu.Unlock()
					return
				}
			}
			next.ClientProfiles = append(next.ClientProfiles[:index], next.ClientProfiles[index+1:]...)
			result = gin.H{"deleted": ref}
		} else {
			var body struct {
				Label    string                           `json:"label"`
				Policies map[string]clientprofiles.Policy `json:"policies"`
			}
			if !profileBody(c, &body) {
				h.mu.Unlock()
				return
			}
			p := clientprofiles.Profile{Ref: ref, Label: body.Label, Policies: body.Policies, Revision: next.ClientProfiles[index].Revision + 1}
			if err := clientprofiles.ValidateTargets(p, h.profileAccountsLocked()); err != nil {
				profileError(c, 422, err)
				h.mu.Unlock()
				return
			}
			next.ClientProfiles[index] = p
			result = p
		}
	}
	h.finishProfileSaveLocked(c, raw, next, result)
}
func (h *Handler) ClientProfilePreview(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, revision, ok := h.profileRevisionLocked(c, false)
	if !ok {
		return
	}
	var body struct {
		ProfileRef string                           `json:"profile_ref"`
		Policies   map[string]clientprofiles.Policy `json:"policies"`
	}
	if !profileBody(c, &body) {
		return
	}
	p := clientprofiles.Profile{Ref: uuid.NewString(), Label: "preview", Policies: body.Policies}
	if body.ProfileRef != "" {
		found := false
		for _, existing := range h.cfg.ClientProfiles {
			if existing.Ref == body.ProfileRef {
				p = existing
				found = true
				break
			}
		}
		if !found {
			profileError(c, 404, clientprofiles.Invalid("profile_not_found", body.ProfileRef))
			return
		}
	}
	if err := clientprofiles.Validate([]clientprofiles.Profile{p}, nil, nil); err != nil {
		profileError(c, 422, err)
		return
	}
	states := map[string]string{}
	for provider, policy := range p.Policies {
		states[provider] = clientprofiles.Resolve(provider, policy, h.profileAccountsLocked())
	}
	c.JSON(200, gin.H{"revision": revision, "policies": p.Policies, "target_states": states, "enforcement": false, "strict_requests": "rejected", "session_behavior": "fresh_session_required"})
}
func (h *Handler) ClientProfileAccounts(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.Request.Method == http.MethodGet {
		c.JSON(200, gin.H{"accounts": h.profileAccountsLocked()})
		return
	}
	if h.authManager == nil {
		profileError(c, 503, clientprofiles.Invalid("credential_owner_unavailable", ""))
		return
	}
	var body struct {
		CredentialRef string `json:"credential_ref"`
	}
	if !profileBody(c, &body) {
		return
	}
	for _, a := range h.authManager.List() {
		if clientprofiles.CredentialRef(a.ID) == body.CredentialRef {
			enrolled, err := h.authManager.EnrollAccountReference(c.Request.Context(), a.ID)
			if err != nil {
				status := 500
				var domain *clientprofiles.Error
				if errors.As(err, &domain) {
					status = 409
				}
				profileError(c, status, err)
				return
			}
			if enrolled == nil {
				profileError(c, 404, clientprofiles.Invalid("credential_removed", ""))
				return
			}
			c.JSON(200, gin.H{"credential_ref": body.CredentialRef, "account_ref": enrolled.Metadata[clientprofiles.AccountRefMetadataKey]})
			return
		}
	}
	profileError(c, 404, clientprofiles.Invalid("credential_not_found", body.CredentialRef))
}
func (h *Handler) ClientProfileKeys(c *gin.Context) {
	h.mu.Lock()
	raw, _, ok := h.profileRevisionLocked(c, c.Request.Method != http.MethodGet)
	if !ok {
		h.mu.Unlock()
		return
	}
	if c.Request.Method == http.MethodGet {
		c.JSON(200, gin.H{"keys": h.cfg.ClientProfileKeys})
		h.mu.Unlock()
		return
	}
	next := h.cfg.CloneForRuntime()
	var result any
	if c.Request.Method == http.MethodPost {
		var body struct {
			Label      string `json:"label"`
			ProfileRef string `json:"profile_ref"`
			APIKey     string `json:"api_key"`
		}
		if !profileBody(c, &body) {
			h.mu.Unlock()
			return
		}
		key := clientprofiles.Key{Ref: uuid.NewString(), Label: body.Label, ProfileRef: body.ProfileRef, Fingerprint: clientprofiles.Fingerprint(body.APIKey), Revision: 1}
		next.ClientProfileKeys = append(next.ClientProfileKeys, key)
		next.RevokedClientProfileKeys = clearRevokedProfileKey(next.RevokedClientProfileKeys, key.Fingerprint)
		result = key
	} else {
		index := -1
		for i, key := range next.ClientProfileKeys {
			if key.Ref == c.Param("ref") {
				index = i
				break
			}
		}
		if index < 0 {
			profileError(c, 404, clientprofiles.Invalid("key_not_found", c.Param("ref")))
			h.mu.Unlock()
			return
		}
		key := next.ClientProfileKeys[index]
		if c.Request.Method == http.MethodDelete {
			next.ClientProfileKeys = append(next.ClientProfileKeys[:index], next.ClientProfileKeys[index+1:]...)
			filtered := next.APIKeys[:0]
			for _, value := range next.APIKeys {
				if clientprofiles.Fingerprint(value) != key.Fingerprint {
					filtered = append(filtered, value)
				}
			}
			if len(filtered) == 0 {
				profileError(c, 409, clientprofiles.Invalid("last_client_key", key.Ref))
				h.mu.Unlock()
				return
			}
			next.APIKeys = filtered
			next.RevokedClientProfileKeys = append(next.RevokedClientProfileKeys, key.Fingerprint)
			result = gin.H{"deleted": key.Ref}
		} else {
			var body struct {
				APIKey     string `json:"api_key"`
				Label      string `json:"label"`
				ProfileRef string `json:"profile_ref"`
			}
			if !profileBody(c, &body) {
				h.mu.Unlock()
				return
			}
			if strings.TrimSpace(body.APIKey) != "" {
				newFingerprint := clientprofiles.Fingerprint(body.APIKey)
				for _, value := range next.APIKeys {
					if clientprofiles.Fingerprint(value) == newFingerprint {
						profileError(c, 409, clientprofiles.Invalid("key_already_exists", key.Ref))
						h.mu.Unlock()
						return
					}
				}
				for i, value := range next.APIKeys {
					if clientprofiles.Fingerprint(value) == key.Fingerprint {
						next.APIKeys[i] = strings.TrimSpace(body.APIKey)
					}
				}
				next.RevokedClientProfileKeys = append(next.RevokedClientProfileKeys, key.Fingerprint)
				next.RevokedClientProfileKeys = clearRevokedProfileKey(next.RevokedClientProfileKeys, newFingerprint)
				key.Fingerprint = newFingerprint
			}
			if body.Label != "" {
				key.Label = body.Label
			}
			if body.ProfileRef != "" {
				key.ProfileRef = body.ProfileRef
			}
			key.Revision++
			next.ClientProfileKeys[index] = key
			result = key
		}
	}
	h.finishProfileSaveLocked(c, raw, next, result)
}

func (h *Handler) finishProfileSaveLocked(c *gin.Context, raw []byte, next *config.Config, result any) {
	if err := next.ValidateClientProfiles(); err != nil {
		profileError(c, 422, err)
		h.mu.Unlock()
		return
	}
	if (len(next.ClientProfileKeys) > 0 || len(h.cfg.ClientProfileKeys) > 0) && h.clientProfilePublisher == nil {
		profileError(c, 503, clientprofiles.Invalid("activation_unavailable", "access"))
		h.mu.Unlock()
		return
	}
	data, err := profileConfigData(raw, next)
	if err == nil {
		err = atomicProfileWrite(h.configFilePath, raw, data)
	}
	if err != nil {
		status := 500
		var domain *clientprofiles.Error
		if errors.As(err, &domain) && domain.Code == "stale_revision" {
			status = 412
		}
		profileError(c, status, err)
		h.mu.Unlock()
		return
	}
	h.cfg = next
	if h.clientProfilePublisher != nil {
		h.clientProfilePublisher(next)
	}
	snapshot := h.reloadSnapshotConfigLocked()
	h.mu.Unlock()
	h.reloadConfigAfterManagementSave(c.Request.Context(), snapshot)
	c.Header("ETag", `"`+clientprofiles.Fingerprint(string(data))+`"`)
	c.JSON(200, gin.H{"revision": `"` + clientprofiles.Fingerprint(string(data)) + `"`, "result": result, "session_behavior": "fresh_session_required"})
}
func profileConfigData(raw []byte, next *config.Config) ([]byte, error) {
	data, _, err := config.NormalizeConfigLayout(raw, true)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err = yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	for name, value := range map[string]any{"client-profiles": next.ClientProfiles, "client-profile-keys": next.ClientProfileKeys, "api-keys": next.APIKeys, "revoked-client-profile-keys": next.RevokedClientProfileKeys} {
		var node yaml.Node
		if err = node.Encode(value); err != nil {
			return nil, err
		}
		access := configV8Node(doc.Content[0], []string{"access"})
		if access == nil {
			access = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			doc.Content[0].Content = append(doc.Content[0].Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "access"}, access)
		}
		old := configV8Node(access, []string{name})
		if old != nil {
			*old = node
		} else {
			access.Content = append(access.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, &node)
		}
	}
	data, err = yaml.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	if err = config.ValidateV8Config(data); err != nil {
		return nil, err
	}
	return data, nil
}
func atomicProfileWrite(path string, before, data []byte) error {
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(current) != string(before) {
		return clientprofiles.Invalid("stale_revision", "config")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".client-profiles-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(info.Mode().Perm()); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	current, err = os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(current) != string(before) {
		return clientprofiles.Invalid("stale_revision", "config")
	}
	return os.Rename(temp, path)
}

// SetClientProfilePublisher installs the synchronous access-policy activation boundary.
func (h *Handler) SetClientProfilePublisher(publish func(*config.Config)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clientProfilePublisher = publish
}

func sameProfileSlice[T any](a, b []T) bool {
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b)
}

func clearRevokedProfileKey(keys []string, fingerprint string) []string {
	result := keys[:0]
	for _, key := range keys {
		if key != fingerprint {
			result = append(result, key)
		}
	}
	return result
}
