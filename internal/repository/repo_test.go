package repository

import (
	"fmt"
	"testing"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/gobwas/glob"
)

func TestRepoShallow(t *testing.T) {
	r, err := git.PlainClone("repo", true, &git.CloneOptions{
		URL:          "https://github.com/googleapis/googleapis",
		Tags:         git.NoTags,
		Depth:        1,
		SingleBranch: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	r.Fetch(&git.FetchOptions{})

	fmt.Println(r)
}

func TestRemoteHead(t *testing.T) {
	strg := filesystem.NewStorage(osfs.New("./repo"), nil)
	rmt := git.NewRemote(strg, &config.RemoteConfig{
		URLs: []string{"https://github.com/googleapis/googleapis"},
	})

	refs, err := rmt.List(&git.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var head *plumbing.Reference
	for _, ref := range refs {
		if ref.Name() == plumbing.HEAD {
			head = ref
		}
	}

	rmtName := "refs/remotes/origin/" + head.Name().Short()

	// just fetch
	err = rmt.Fetch(&git.FetchOptions{
		Depth: 1,
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("+%s:%s", head.Target(), rmtName)),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFilesFilter(t *testing.T) {
	repo := NewRepository("https://github.com/googleapis/googleapis", "./repo", nil, false)

	filter, err := glob.Compile("**.proto")
	if err != nil {
		t.Fatal(err)
	}

	_, files, err := repo.Files("a3211f3", "", filter)
	if err != nil {
		t.Fatal(err)
	}

	for _, file := range files {
		fmt.Println(file.Name)
		break
	}
}

func TestNotShallow(t *testing.T) {
	repo := NewRepository("https://github.com/googleapis/googleapis", "./repo", nil, false)
	filter, err := glob.Compile("**.proto")
	if err != nil {
		t.Fatal(err)
	}
	_, files, err := repo.FilesCommit("a3211f3", "", filter)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		fmt.Println(file.Name)
		break
	}
}

func TestShallow(t *testing.T) {
	repo := NewRepository("https://github.com/googleapis/googleapis", "./repo", nil, true)
	filter, err := glob.Compile("**.proto")
	if err != nil {
		t.Fatal(err)
	}
	_, files, err := repo.FilesCommit("27156597f", "", filter)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		fmt.Println(file.Name)
		break
	}
}

func TestShallowBranch(t *testing.T) {
	repo := NewRepository("https://github.com/googleapis/googleapis", "./repo", nil, true)
	filter, err := glob.Compile("**.proto")
	if err != nil {
		t.Fatal(err)
	}
	_, files, err := repo.Files("master", "", filter)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		fmt.Println(file.Name)
		break
	}
}

func TestNotShallowBranch(t *testing.T) {
	repo := NewRepository("https://github.com/googleapis/googleapis", "./repo", nil, false)
	filter, err := glob.Compile("**.proto")
	if err != nil {
		t.Fatal(err)
	}
	_, files, err := repo.Files("master", "", filter)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		fmt.Println(file.Name)
		break
	}
}

func TestShallowHead(t *testing.T) {
	repo := NewRepository("https://github.com/googleapis/googleapis", "./repo", nil, true)
	filter, err := glob.Compile("**.proto")
	if err != nil {
		t.Fatal(err)
	}
	_, files, err := repo.Files("", "", filter)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		fmt.Println(file.Name)
		break
	}
}

func TestShallowTag(t *testing.T) {
	repo := NewRepository("https://github.com/googleapis/googleapis", "./repo", nil, true)
	filter, err := glob.Compile("**.proto")
	if err != nil {
		t.Fatal(err)
	}
	_, files, err := repo.Files("common-protos-1_3_1", "", filter)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		fmt.Println(file.Name)
		break
	}
}
