package repository

import (
	"fmt"
	"os"
	"testing"
)

func TestPlainClone(t *testing.T) {
	tok := os.Getenv("GITHUB_TOKEN")
	if tok == "" {
		t.Fatal("GITHUB_TOKEN not set")
	}
	repo, err := New("https://github.com/greatliontech/test-pbr", tok, 60)
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
