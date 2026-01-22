package service

import (
	"context"
	"testing"

	registryv1alpha1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/registry/v1alpha1"
	"connectrpc.com/connect"
)

func TestAuthnService_GetCurrentUser(t *testing.T) {
	// Create a mock service
	svc := &Service{
		tokens: map[string]*tokenInfo{
			"test-token": {Username: "testuser"},
		},
		users: map[string]string{
			"testuser": "test-token",
		},
	}
	authnSvc := NewAuthnService(svc)

	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{
			name:     "valid user",
			username: "testuser",
			wantErr:  false,
		},
		{
			name:     "empty user",
			username: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.username != "" {
				ctx = contextWithUser(ctx, tt.username)
			}

			req := connect.NewRequest(&registryv1alpha1.GetCurrentUserRequest{})
			resp, err := authnSvc.GetCurrentUser(ctx, req)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetCurrentUser() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("GetCurrentUser() unexpected error: %v", err)
				return
			}

			if resp.Msg.User == nil {
				t.Errorf("GetCurrentUser() returned nil user")
				return
			}

			if resp.Msg.User.Username != tt.username {
				t.Errorf("GetCurrentUser() username = %v, want %v", resp.Msg.User.Username, tt.username)
			}
		})
	}
}

func TestAuthnService_GetCurrentUserSubject(t *testing.T) {
	svc := &Service{
		tokens: map[string]*tokenInfo{
			"test-token": {Username: "testuser"},
		},
		users: map[string]string{
			"testuser": "test-token",
		},
	}
	authnSvc := NewAuthnService(svc)

	tests := []struct {
		name        string
		username    string
		wantSubject string
		wantErr     bool
	}{
		{
			name:        "valid user",
			username:    "testuser",
			wantSubject: "testuser",
			wantErr:     false,
		},
		{
			name:     "empty user",
			username: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.username != "" {
				ctx = contextWithUser(ctx, tt.username)
			}

			req := connect.NewRequest(&registryv1alpha1.GetCurrentUserSubjectRequest{})
			resp, err := authnSvc.GetCurrentUserSubject(ctx, req)

			if tt.wantErr {
				if err == nil {
					t.Errorf("GetCurrentUserSubject() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("GetCurrentUserSubject() unexpected error: %v", err)
				return
			}

			if resp.Msg.Subject != tt.wantSubject {
				t.Errorf("GetCurrentUserSubject() subject = %v, want %v", resp.Msg.Subject, tt.wantSubject)
			}
		})
	}
}

func TestGenerateUserID(t *testing.T) {
	tests := []struct {
		name     string
		username string
	}{
		{
			name:     "simple username",
			username: "testuser",
		},
		{
			name:     "email as username",
			username: "test@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id1 := generateUserID(tt.username)
			id2 := generateUserID(tt.username)

			// IDs should be deterministic
			if id1 != id2 {
				t.Errorf("generateUserID() not deterministic: %v != %v", id1, id2)
			}

			// ID should be 32 chars (16 bytes hex encoded)
			if len(id1) != 32 {
				t.Errorf("generateUserID() length = %v, want 32", len(id1))
			}
		})
	}

	// Different usernames should produce different IDs
	id1 := generateUserID("user1")
	id2 := generateUserID("user2")
	if id1 == id2 {
		t.Errorf("generateUserID() produced same ID for different usernames")
	}
}
