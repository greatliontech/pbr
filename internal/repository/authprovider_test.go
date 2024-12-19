package repository

import (
	"fmt"
	"os"
	"testing"
	"time"

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

	for i := 0; i < 12; i++ {
		auth, err := ghap.AuthMethod()
		if err != nil {
			t.Fatalf("Failed to create auth method with github app: %s", err)
		}
		basicAuth, ok := auth.(*githttp.BasicAuth)
		if !ok {
			t.Fatalf("Expected BasicAuth, got %T", auth)
		}
		fmt.Println(basicAuth.Username, basicAuth.Password)
		time.Sleep(1 * time.Minute)
	}
}
