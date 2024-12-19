package repository

import (
	"fmt"
	"os"
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func TestGithubApp(t *testing.T) {
	key := os.Getenv("PBR_TEST_GH_KEY")
	if key == "" {
		t.Skip("PBR_TEST_GH_KEY not set")
	}

	ghap := &GithubAppAuthProvider{
		AppID:          1072076,
		InstallationID: 57714146,
		PrivateKey:     []byte(key),
	}

	auth, err := ghap.AuthMethod()
	if err != nil {
		t.Fatalf("Failed to create auth method with github app: %s", err)
	}

	httpAuth, ok := auth.(*githttp.BasicAuth)
	if !ok {
		t.Fatalf("Expected BasicAuth, got %T", auth)
	}

	fmt.Println(httpAuth.Username, httpAuth.Password)
}
