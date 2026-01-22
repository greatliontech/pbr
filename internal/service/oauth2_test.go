package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/greatliontech/pbr/internal/config"
)

func TestOAuth2_DeviceRegistration_NoOIDC(t *testing.T) {
	// Create service without OIDC configured
	svc := &Service{
		conf: &config.Config{
			Host: "pbr.test",
		},
		tokens: make(map[string]*tokenInfo),
		users:  make(map[string]string),
	}

	oauth2Svc := &OAuth2Service{
		svc:  svc,
		oidc: nil, // No OIDC configured
	}
	handler := oauth2Svc.Handler()

	reqBody := DeviceRegistrationRequest{
		ClientName: "test-client",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, DeviceRegistrationPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("DeviceRegistration() without OIDC status = %v, want %v", rec.Code, http.StatusServiceUnavailable)
	}

	var errResp OAuth2Error
	if err := json.NewDecoder(rec.Body).Decode(&errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errResp.ErrorCode != "server_error" {
		t.Errorf("DeviceRegistration() error = %v, want server_error", errResp.ErrorCode)
	}
}

func TestOAuth2_DeviceRegistration_MethodNotAllowed(t *testing.T) {
	svc := &Service{
		conf: &config.Config{
			Host: "pbr.test",
		},
		tokens: make(map[string]*tokenInfo),
		users:  make(map[string]string),
	}

	oauth2Svc := &OAuth2Service{
		svc:  svc,
		oidc: nil,
	}
	handler := oauth2Svc.Handler()

	// GET instead of POST
	req := httptest.NewRequest(http.MethodGet, DeviceRegistrationPath, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DeviceRegistration() GET status = %v, want %v", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestOAuth2_DeviceAuthorization_NoOIDC(t *testing.T) {
	svc := &Service{
		conf: &config.Config{
			Host: "pbr.test",
		},
		tokens: make(map[string]*tokenInfo),
		users:  make(map[string]string),
	}

	oauth2Svc := &OAuth2Service{
		svc:  svc,
		oidc: nil,
	}
	handler := oauth2Svc.Handler()

	req := httptest.NewRequest(http.MethodPost, DeviceAuthorizationPath, nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("DeviceAuthorization() without OIDC status = %v, want %v", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestOAuth2_DeviceAuthorization_MethodNotAllowed(t *testing.T) {
	svc := &Service{
		conf: &config.Config{
			Host: "pbr.test",
		},
		tokens: make(map[string]*tokenInfo),
		users:  make(map[string]string),
	}

	oauth2Svc := &OAuth2Service{
		svc:  svc,
		oidc: nil,
	}
	handler := oauth2Svc.Handler()

	req := httptest.NewRequest(http.MethodGet, DeviceAuthorizationPath, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DeviceAuthorization() GET status = %v, want %v", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestOAuth2_DeviceToken_NoOIDC(t *testing.T) {
	svc := &Service{
		conf: &config.Config{
			Host: "pbr.test",
		},
		tokens: make(map[string]*tokenInfo),
		users:  make(map[string]string),
	}

	oauth2Svc := &OAuth2Service{
		svc:  svc,
		oidc: nil,
	}
	handler := oauth2Svc.Handler()

	req := httptest.NewRequest(http.MethodPost, DeviceTokenPath, nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("DeviceToken() without OIDC status = %v, want %v", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestOAuth2_DeviceToken_MethodNotAllowed(t *testing.T) {
	svc := &Service{
		conf: &config.Config{
			Host: "pbr.test",
		},
		tokens: make(map[string]*tokenInfo),
		users:  make(map[string]string),
	}

	oauth2Svc := &OAuth2Service{
		svc:  svc,
		oidc: nil,
	}
	handler := oauth2Svc.Handler()

	req := httptest.NewRequest(http.MethodGet, DeviceTokenPath, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DeviceToken() GET status = %v, want %v", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestGenerateRandomString(t *testing.T) {
	tests := []struct {
		length int
	}{
		{length: 16},
		{length: 32},
		{length: 64},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			s := generateRandomString(tt.length)
			if len(s) != tt.length {
				t.Errorf("generateRandomString(%d) length = %v, want %v", tt.length, len(s), tt.length)
			}
		})
	}

	// Test uniqueness (basic sanity check)
	generated := make(map[string]bool)
	for i := 0; i < 100; i++ {
		s := generateRandomString(32)
		if generated[s] {
			t.Errorf("generateRandomString() produced duplicate")
		}
		generated[s] = true
	}
}
