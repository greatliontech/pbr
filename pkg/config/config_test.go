package config

import (
	"os"
	"testing"
)

func TestParseValidConfig(t *testing.T) {
	yamlData := []byte(`
credentials:
  git:
		https://github.com/
      token: "tokenValue"
      sshKey: "sshKeyValue"
modules:
  module1:
    remote: "remotePath"
    path: "localPath"
plugins:
  plugin1:
    image: "imageName"
host: "localhost"
address: ":8080"
`)

	config, err := ParseConfig(yamlData)
	if err != nil {
		t.Errorf("Failed to parse valid config: %s", err)
	}

	if config.Host != "localhost" {
		t.Errorf("Expected host 'localhost', got '%s'", config.Host)
	}

	if config.Address != ":8080" {
		t.Errorf("Expected address ':8080', got '%s'", config.Address)
	}

	if config.Modules["module1"].Remote != "remotePath" {
		t.Errorf("Expected module1 remote 'remotePath', got '%s'", config.Modules["module1"].Remote)
	}

	if config.Modules["module1"].Path != "localPath" {
		t.Errorf("Expected module1 path 'localPath', got '%s'", config.Modules["module1"].Path)
	}

	if config.Modules["module1"].Filters != nil {
		t.Errorf("Expected module1 filters nil, got '%s'", config.Modules["module1"].Filters)
	}

	if config.Plugins["plugin1"].Image != "imageName" {
		t.Errorf("Expected plugin1 image 'imageName', got '%s'", config.Plugins["plugin1"].Image)
	}

	if config.Credentials.Git["gitKey"].Token != "tokenValue" {
		t.Errorf("Expected git gitKey token 'tokenValue', got '%s'", config.Credentials.Git["gitKey"].Token)
	}
}

func TestEnvVarSubstitution(t *testing.T) {
	// Set an environment variable for testing
	os.Setenv("TEST_TOKEN", "exampleToken")
	defer os.Unsetenv("TEST_TOKEN")

	yamlData := []byte(`
credentials:
  git:
    gitKey:
      token: "${TEST_TOKEN}"
`)

	config, err := ParseConfig(yamlData)
	if err != nil {
		t.Errorf("Failed to parse config with env var: %s", err)
	}

	if config.Credentials.Git["gitKey"].Token != "exampleToken" {
		t.Errorf("Expected token 'exampleToken', got '%s'", config.Credentials.Git["gitKey"].Token)
	}
}

func TestParseInvalidConfig(t *testing.T) {
	invalidYAML := []byte(`:invalidYAML`)

	_, err := ParseConfig(invalidYAML)
	if err == nil {
		t.Error("Expected an error for invalid YAML, but got none")
	}
}

func TestFromFile(t *testing.T) {
	// Create a temporary file with test data
	tempFile, err := os.CreateTemp("", "test_config_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %s", err)
	}
	defer os.Remove(tempFile.Name())

	content := []byte(`host: "localhost"`)
	if _, err := tempFile.Write(content); err != nil {
		t.Fatalf("Failed to write to temp file: %s", err)
	}
	if err := tempFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %s", err)
	}

	config, err := FromFile(tempFile.Name())
	if err != nil {
		t.Errorf("Failed to read from file: %s", err)
	}

	if config.Host != "localhost" {
		t.Errorf("Expected host 'localhost', got '%s'", config.Host)
	}
}
