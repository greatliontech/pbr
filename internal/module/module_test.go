package module

import (
	"fmt"
	"testing"

	"github.com/greatliontech/pbr/internal/repository"
)

func TestModule(t *testing.T) {
	repo := repository.NewRepository("https://github.com/greatliontech/protoc-gen-rtk-query", "./repo", repository.WithShallow())
	module := New("greatliontech", "protoc-gen-rtk-query", repo, "proto", nil)

	files, manifest, err := module.FilesAndManifest("")
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("Files:")
	for _, file := range files {
		fmt.Println(file.Name)
	}

	fmt.Println("Manifest:")
	fmt.Println(manifest)
}

func TestModuleGoogleapis(t *testing.T) {
	repo := repository.NewRepository("https://github.com/googleapis/googleapis", "./repo")
	module := New("googleapis", "googleapis", repo, "", nil)

	files, manifest, err := module.FilesAndManifest("")
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("Files:")
	for _, file := range files {
		fmt.Println(file.Name)
	}

	fmt.Println("Manifest:")
	fmt.Println(manifest)
}
