package repository

import (
	"testing"

	"github.com/greatliontech/pbr/pkg/config"
)

const mockSSHKey = `
-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBF9cFkODISuUax6k5eZdRATuGw2Z66xehVhbd4ezIGSwAAAKBDMfKiQzHy
ogAAAAtzc2gtZWQyNTUxOQAAACBF9cFkODISuUax6k5eZdRATuGw2Z66xehVhbd4ezIGSw
AAAEAYLIdIOsm1edw46gSeqQt2PbJ7xQLwKmVf2QinH2MJpkX1wWQ4MhK5RrHqTl5l1EBO
4bDZnrrF6FWFt3h7MgZLAAAAFm5pa29sYXNAamluaXVzLWxhdC01NTIBAgMEBQYH
-----END OPENSSH PRIVATE KEY-----
`

func TestNewCredentialStore_Success(t *testing.T) {
	creds := map[string]config.GitAuth{
		"*.example.com": {
			Token: "exampleToken",
		},
		"ssh.example.com": {
			SSHKey: mockSSHKey,
		},
	}

	cs, err := NewCredentialStore(creds)
	if err != nil {
		t.Fatalf("Failed to create CredentialStore: %s", err)
	}

	// Verify that CredentialStore is initialized correctly
	if len(cs.creds) != 2 {
		t.Errorf("Expected 2 credentials, got %d", len(cs.creds))
	}
	// Additional checks can be added here
}

func TestNewCredentialStore_ErrorHandling(t *testing.T) {
	invalidCreds := map[string]config.GitAuth{
		"[invalid-pattern": {Token: "exampleToken"}, // Invalid glob pattern
	}

	_, err := NewCredentialStore(invalidCreds)
	if err == nil {
		t.Error("Expected an error for invalid credentials, but got none")
	}
}

func TestCredentialStore_Auth(t *testing.T) {
	creds := map[string]config.GitAuth{
		"*.example.com": {
			Token: "exampleToken",
		},
	}

	cs, _ := NewCredentialStore(creds)

	testCases := []struct {
		remote     string
		expectAuth bool
	}{
		{"git.example.com", true},
		{"nonmatching.com", false},
	}

	for _, tc := range testCases {
		auth := cs.Auth(tc.remote)
		if (auth != nil) != tc.expectAuth {
			t.Errorf("Auth for '%s' - expected %t, got %t", tc.remote, tc.expectAuth, auth != nil)
		}
	}
}
