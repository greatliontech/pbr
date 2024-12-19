package registry

import (
	"fmt"
	"testing"

	"github.com/greatliontech/pbr/internal/repository"
)

func TestModule(t *testing.T) {
	repo := repository.NewRepository("https://github.com/greatliontech/protoc-gen-rtk-query", "./repo", nil, true)
	module, err := NewModule("greatliontech", "protoc-gen-rtk-query", repo, "proto", nil)
	if err != nil {
		t.Fatal(err)
	}

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
	repo := repository.NewRepository("https://github.com/googleapis/googleapis", "./repo", nil, false)
	module, err := NewModule("googleapis", "googleapis", repo, "", nil)
	if err != nil {
		t.Fatal(err)
	}

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
