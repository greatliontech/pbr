package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/greatliontech/pbr/internal/config"
)

// OIDCProvider handles OpenID Connect authentication.
type OIDCProvider struct {
	config    *config.OIDC
	discovery *OIDCDiscovery
	host      string
}

// OIDCDiscovery holds the OIDC provider's discovery document.
type OIDCDiscovery struct {
	Issuer                      string `json:"issuer"`
	AuthorizationEndpoint       string `json:"authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
	UserinfoEndpoint            string `json:"userinfo_endpoint"`
	JwksURI                     string `json:"jwks_uri"`
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
}

// UserInfo represents the user info response from OIDC provider.
type UserInfo struct {
	Subject           string `json:"sub"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	Name              string `json:"name"`
	EmailVerified     bool   `json:"email_verified"`
}

// NewOIDCProvider creates a new OIDC provider.
func NewOIDCProvider(cfg *config.OIDC, host string) (*OIDCProvider, error) {
	if cfg == nil {
		return nil, nil
	}

	provider := &OIDCProvider{
		config: cfg,
		host:   host,
	}

	// Discover OIDC endpoints
	discovery, err := provider.discover(context.Background())
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed: %w", err)
	}
	provider.discovery = discovery

	// Verify device authorization endpoint is available
	if discovery.DeviceAuthorizationEndpoint == "" {
		return nil, fmt.Errorf("OIDC provider does not support device authorization grant (no device_authorization_endpoint in discovery)")
	}

	slog.Info("OIDC provider configured",
		"issuer", cfg.Issuer,
		"device_auth_endpoint", discovery.DeviceAuthorizationEndpoint,
		"userinfo_endpoint", discovery.UserinfoEndpoint,
	)

	return provider, nil
}

// discover fetches the OIDC discovery document.
func (p *OIDCProvider) discover(ctx context.Context) (*OIDCDiscovery, error) {
	discoveryURL := strings.TrimSuffix(p.config.Issuer, "/") + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discovery request failed: %d %s", resp.StatusCode, string(body))
	}

	var discovery OIDCDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, err
	}

	return &discovery, nil
}

// GetScopes returns the OIDC scopes to request.
func (p *OIDCProvider) GetScopes() []string {
	if len(p.config.Scopes) > 0 {
		return p.config.Scopes
	}
	return []string{"openid", "email", "profile"}
}

// GetUsernameClaim returns the claim to use as username.
func (p *OIDCProvider) GetUsernameClaim() string {
	if p.config.UsernameClaim != "" {
		return p.config.UsernameClaim
	}
	return "preferred_username"
}

// GetUserInfo fetches user information from the OIDC provider using an access token.
func (p *OIDCProvider) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	if p.discovery.UserinfoEndpoint == "" {
		return nil, fmt.Errorf("OIDC provider does not have userinfo endpoint")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.discovery.UserinfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo request failed: %d %s", resp.StatusCode, string(body))
	}

	var userInfo UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

// ExtractUsername extracts the username from user info based on configured claim.
func (p *OIDCProvider) ExtractUsername(userInfo *UserInfo) string {
	claim := p.GetUsernameClaim()

	switch claim {
	case "preferred_username":
		return userInfo.PreferredUsername
	case "email":
		return userInfo.Email
	case "name":
		return userInfo.Name
	case "sub":
		return userInfo.Subject
	default:
		// Try preferred_username first, then email, then sub
		if userInfo.PreferredUsername != "" {
			return userInfo.PreferredUsername
		}
		if userInfo.Email != "" {
			return userInfo.Email
		}
		return userInfo.Subject
	}
}
