package repository

import (
	"fmt"
	"os"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

func TestPlainClone(t *testing.T) {
	tok := os.Getenv("GITHUB_TOKEN")
	if tok == "" {
		t.Fatal("GITHUB_TOKEN not set")
	}

	httpAuth := &http.BasicAuth{
		Username: "git",
		Password: tok,
	}

	repo, err := New("https://github.com/greatliontech/test-pbr", WithAuth(httpAuth))
	if err != nil {
		t.Fatal(err)
	}

	files, mani, err := repo.FilesAndManifest("branch")
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range files {
		fmt.Println(file)
	}

	fmt.Println(mani)
}
