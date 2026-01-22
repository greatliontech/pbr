package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// OAuth2 paths as defined by buf CLI
	DeviceRegistrationPath  = "/oauth2/device/registration"
	DeviceAuthorizationPath = "/oauth2/device/authorization"
	DeviceTokenPath         = "/oauth2/device/token"
)

// DeviceRegistrationRequest is the request for device registration.
type DeviceRegistrationRequest struct {
	ClientName string `json:"client_name"`
}

// DeviceRegistrationResponse is the response for device registration.
type DeviceRegistrationResponse struct {
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret,omitempty"`
	ClientIDIssuedAt      int64  `json:"client_id_issued_at"`
	ClientSecretExpiresAt int64  `json:"client_secret_expires_at,omitempty"`
}

// DeviceAccessTokenResponse is the response for device access token.
type DeviceAccessTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// OAuth2Error is an error response.
type OAuth2Error struct {
	ErrorCode        string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func (e *OAuth2Error) Error() string {
	return e.ErrorCode + ": " + e.ErrorDescription
}

// OAuth2Service handles OAuth2 device authorization flow by proxying to OIDC provider.
type OAuth2Service struct {
	svc  *Service
	oidc *OIDCProvider
}

// NewOAuth2Service creates a new OAuth2Service.
func NewOAuth2Service(svc *Service) *OAuth2Service {
	o := &OAuth2Service{
		svc: svc,
	}

	// Initialize OIDC provider if configured
	if svc.conf.OIDC != nil {
		oidc, err := NewOIDCProvider(svc.conf.OIDC, svc.conf.Host)
		if err != nil {
			slog.Error("Failed to initialize OIDC provider", "error", err)
		} else {
			o.oidc = oidc
		}
	}

	return o
}

// Handler returns an HTTP handler for OAuth2 endpoints.
func (o *OAuth2Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(DeviceRegistrationPath, o.handleDeviceRegistration)
	mux.HandleFunc(DeviceAuthorizationPath, o.handleDeviceAuthorization)
	mux.HandleFunc(DeviceTokenPath, o.handleDeviceToken)
	return mux
}

// handleDeviceRegistration returns the configured OIDC client_id.
// No proxy needed - we just return what's in our config.
func (o *OAuth2Service) handleDeviceRegistration(w http.ResponseWriter, r *http.Request) {
	slog.Debug("handleDeviceRegistration called", "method", r.Method, "path", r.URL.Path)

	if r.Method != http.MethodPost {
		slog.Debug("handleDeviceRegistration: method not allowed", "method", r.Method)
		writeOAuth2Error(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	// If OIDC is not configured, return error
	if o.oidc == nil {
		slog.Debug("handleDeviceRegistration: OIDC not configured")
		writeOAuth2Error(w, http.StatusServiceUnavailable, "server_error", "OIDC not configured")
		return
	}

	// Return the configured client_id from OIDC config
	resp := DeviceRegistrationResponse{
		ClientID:         o.svc.conf.OIDC.ClientID,
		ClientIDIssuedAt: time.Now().Unix(),
	}

	slog.Debug("handleDeviceRegistration: returning response", "client_id", resp.ClientID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleDeviceAuthorization proxies to OIDC provider's device authorization endpoint.
func (o *OAuth2Service) handleDeviceAuthorization(w http.ResponseWriter, r *http.Request) {
	slog.Debug("handleDeviceAuthorization called", "method", r.Method, "path", r.URL.Path)

	if r.Method != http.MethodPost {
		slog.Debug("handleDeviceAuthorization: method not allowed", "method", r.Method)
		writeOAuth2Error(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	if o.oidc == nil {
		slog.Debug("handleDeviceAuthorization: OIDC not configured")
		writeOAuth2Error(w, http.StatusServiceUnavailable, "server_error", "OIDC not configured")
		return
	}

	// Read the incoming request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Debug("handleDeviceAuthorization: failed to read request body", "error", err)
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "failed to read request")
		return
	}
	slog.Debug("handleDeviceAuthorization: request body", "body", string(body))

	// Parse the form data to potentially add client_secret
	values, err := url.ParseQuery(string(body))
	if err != nil {
		slog.Debug("handleDeviceAuthorization: invalid form data", "error", err)
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "invalid form data")
		return
	}

	// Add client_secret if configured (some providers require it)
	if o.svc.conf.OIDC.ClientSecret != "" {
		values.Set("client_secret", o.svc.conf.OIDC.ClientSecret)
	}

	// Add scopes if not present
	if values.Get("scope") == "" {
		values.Set("scope", strings.Join(o.oidc.GetScopes(), " "))
	}

	slog.Debug("handleDeviceAuthorization: proxying to OIDC provider",
		"endpoint", o.oidc.discovery.DeviceAuthorizationEndpoint,
		"client_id", values.Get("client_id"),
		"scope", values.Get("scope"))

	// Proxy to OIDC provider's device authorization endpoint
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		o.oidc.discovery.DeviceAuthorizationEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		slog.Debug("handleDeviceAuthorization: failed to create proxy request", "error", err)
		writeOAuth2Error(w, http.StatusInternalServerError, "server_error", "failed to create proxy request")
		return
	}
	proxyReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	proxyReq.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		slog.Error("Failed to proxy device authorization", "error", err)
		writeOAuth2Error(w, http.StatusBadGateway, "server_error", "failed to contact OIDC provider")
		return
	}
	defer resp.Body.Close()

	// Read response body to normalize field names
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug("handleDeviceAuthorization: failed to read provider response", "error", err)
		writeOAuth2Error(w, http.StatusInternalServerError, "server_error", "failed to read provider response")
		return
	}

	slog.Debug("handleDeviceAuthorization: OIDC provider response",
		"status", resp.StatusCode,
		"body", string(respBody))

	// If successful, normalize the response to include both verification_uri and verification_url
	// Google uses verification_url but RFC 8628 specifies verification_uri
	if resp.StatusCode == http.StatusOK {
		var deviceResp map[string]interface{}
		if err := json.Unmarshal(respBody, &deviceResp); err == nil {
			slog.Debug("handleDeviceAuthorization: parsed response",
				"verification_uri", deviceResp["verification_uri"],
				"verification_url", deviceResp["verification_url"])

			// Normalize: if we have verification_url but not verification_uri, add it
			if url, ok := deviceResp["verification_url"].(string); ok {
				if _, hasURI := deviceResp["verification_uri"]; !hasURI {
					deviceResp["verification_uri"] = url
					slog.Debug("handleDeviceAuthorization: added verification_uri from verification_url")
				}
			}
			// Normalize: if we have verification_uri but not verification_url, add it
			if uri, ok := deviceResp["verification_uri"].(string); ok {
				if _, hasURL := deviceResp["verification_url"]; !hasURL {
					deviceResp["verification_url"] = uri
					slog.Debug("handleDeviceAuthorization: added verification_url from verification_uri")
				}
			}

			finalResp, _ := json.Marshal(deviceResp)
			slog.Debug("handleDeviceAuthorization: sending response", "response", string(finalResp))

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			json.NewEncoder(w).Encode(deviceResp)
			return
		} else {
			slog.Debug("handleDeviceAuthorization: failed to parse response as JSON", "error", err)
		}
	}

	// Pass through error responses as-is
	slog.Debug("handleDeviceAuthorization: passing through error response", "status", resp.StatusCode)
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// handleDeviceToken proxies to OIDC provider, then swaps the token for a PBR token.
func (o *OAuth2Service) handleDeviceToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuth2Error(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	if o.oidc == nil {
		writeOAuth2Error(w, http.StatusServiceUnavailable, "server_error", "OIDC not configured")
		return
	}

	// Read the incoming request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "failed to read request")
		return
	}

	// Parse and add client_secret if configured
	values, err := url.ParseQuery(string(body))
	if err != nil {
		writeOAuth2Error(w, http.StatusBadRequest, "invalid_request", "invalid form data")
		return
	}

	if o.svc.conf.OIDC.ClientSecret != "" {
		values.Set("client_secret", o.svc.conf.OIDC.ClientSecret)
	}

	// Proxy to OIDC provider's token endpoint
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		o.oidc.discovery.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		writeOAuth2Error(w, http.StatusInternalServerError, "server_error", "failed to create proxy request")
		return
	}
	proxyReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	proxyReq.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		slog.Error("Failed to proxy token request", "error", err)
		writeOAuth2Error(w, http.StatusBadGateway, "server_error", "failed to contact OIDC provider")
		return
	}
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		writeOAuth2Error(w, http.StatusInternalServerError, "server_error", "failed to read provider response")
		return
	}

	// If not successful, pass through the error response
	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		w.Write(respBody)
		return
	}

	// Parse the OIDC token response
	var oidcTokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(respBody, &oidcTokenResp); err != nil {
		slog.Error("Failed to parse OIDC token response", "error", err)
		writeOAuth2Error(w, http.StatusInternalServerError, "server_error", "failed to parse provider response")
		return
	}

	// Get user info from OIDC provider using the access token
	userInfo, err := o.oidc.GetUserInfo(r.Context(), oidcTokenResp.AccessToken)
	if err != nil {
		slog.Error("Failed to get user info", "error", err)
		writeOAuth2Error(w, http.StatusInternalServerError, "server_error", "failed to get user info")
		return
	}

	username := o.oidc.ExtractUsername(userInfo)
	if username == "" {
		slog.Error("Could not extract username from user info", "userinfo", userInfo)
		writeOAuth2Error(w, http.StatusInternalServerError, "server_error", "could not determine username")
		return
	}

	// Generate a PBR token for this user
	pbrToken := generateRandomString(64)
	expiresAt := time.Now().Add(o.svc.conf.GetTokenTTL())

	// Store the token (replace any existing token for this user)
	o.svc.mu.Lock()
	// Delete old token if user already has one
	if oldToken, exists := o.svc.users[username]; exists {
		delete(o.svc.tokens, oldToken)
	}
	o.svc.tokens[pbrToken] = &tokenInfo{
		Username:  username,
		ExpiresAt: expiresAt,
	}
	o.svc.users[username] = pbrToken
	o.svc.mu.Unlock()

	slog.Info("User authenticated via OIDC", "username", username, "expires_at", expiresAt)

	// Return our PBR token instead of the OIDC token
	pbrResp := DeviceAccessTokenResponse{
		AccessToken: pbrToken,
		TokenType:   "Bearer",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(pbrResp)
}

// writeOAuth2Error writes an OAuth2 error response.
func writeOAuth2Error(w http.ResponseWriter, status int, errorCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(&OAuth2Error{
		ErrorCode:        errorCode,
		ErrorDescription: description,
	})
}

// generateRandomString generates a random hex string of the given length.
func generateRandomString(length int) string {
	b := make([]byte, (length+1)/2)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}
