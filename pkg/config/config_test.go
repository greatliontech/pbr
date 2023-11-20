package config

import (
	"os"
	"testing"
)

func TestCorrectConfig(t *testing.T) {
	y := []byte(`
host: bsr.greatlion.tech
modules:
  greatliontech/test-pbr:	
    remote: github.com
credentials:
  bsr:
    buf.build: ${BUF_TOKEN}
  git:
    github.com/greatliontech/*: ${GITHUB_TOKEN}
`)

	if err := os.Setenv("BUF_TOKEN", "bufsecret"); err != nil {
		t.Fatal(err)
	}

	if err := os.Setenv("GITHUB_TOKEN", "githubsecret"); err != nil {
		t.Fatal(err)
	}

	c, err := ParseConfig(y)
	if err != nil {
		t.Fatal(err)
	}

	if c.Host != "bsr.greatlion.tech" {
		t.Errorf("expected bsr.greatlion.tech, got %s", c.Host)
	}

	if len(c.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(c.Modules))
	}

	if c.Modules["greatliontech/test-pbr"].Remote != "github.com" {
		t.Errorf("expected github.com as remote, got %s", c.Modules["greatliontech/test-pbr"].Remote)
	}

	if len(c.Credentials.Bsr) != 1 {
		t.Fatalf("expected 1 bsr secret, got %d", len(c.Credentials.Bsr))
	}

	if len(c.Credentials.Git) != 1 {
		t.Fatalf("expected 1 git secret, got %d", len(c.Credentials.Git))
	}

	if c.Credentials.BsrToken("buf.build") != "bufsecret" {
		t.Errorf("expected buf.build secret to be bufsecret, got %s", c.Credentials.Bsr["buf.build"])
	}

	if c.Credentials.GitToken("github.com/greatliontech/test") != "githubsecret" {
		t.Errorf("expected github.com secret to be githubsecret, got %s", c.Credentials.Git["github.com"])
	}
}
