package config

import (
	"os"
	"testing"
)

func TestParseValidConfig(t *testing.T) {
	yamlData := []byte(`
host: "localhost"
address: ":8080"
loglevel: "debug"
admintoken: testTest
users:
  testUser: testPassword
credentials:
  git:
    somehost/*:
      token: "tokenValue"
      sshkey: "sshKeyValue"
modules:
  module1:
    remote: "remotePath"
    path: "localPath"
plugins:
  plugin1:
    image: "imageName"
    default: v1.2.3
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

	if config.Plugins["plugin1"].Default != "v1.2.3" {
		t.Errorf("Expected plugin1 default 'v1.2.3', got '%s'", config.Plugins["plugin1"].Default)
	}

	if config.Credentials.Git["somehost/*"].Token != "tokenValue" {
		t.Errorf("Expected git gitKey token 'tokenValue', got '%s'", config.Credentials.Git["somehost/*"].Token)
	}

	if config.Credentials.Git["somehost/*"].SSHKey != "sshKeyValue" {
		t.Errorf("Expected git gitKey sshKey 'sshKeyValue', got '%s'", config.Credentials.Git["somehost/*"].SSHKey)
	}

	if config.AdminToken != "testTest" {
		t.Errorf("Expected admin token 'testTest', got '%s'", config.AdminToken)
	}

	if config.Users["testUser"] != "testPassword" {
		t.Errorf("Expected user 'testUser' password 'testPassword', got '%s'", config.Users["testUser"])
	}
}

func TestEnvVarSubstitution(t *testing.T) {
	// Set an environment variable for testing
	os.Setenv("TEST_TOKEN", "exampleToken")
	defer os.Unsetenv("TEST_TOKEN")
	os.Setenv("TEST_USER_PASSWORD", "examplePassword")
	defer os.Unsetenv("TEST_USER_PASSWORD")
	os.Setenv("PBR_PASSWORD", "pbrPassword")
	defer os.Unsetenv("PBR_PASSWORD")

	yamlData := []byte(`
users:
  testUser: "${TEST_USER_PASSWORD}"
credentials:
  git:
    gitKey:
      token: "${TEST_TOKEN}"
  containerregistry:
    cr.platform:
      username: pbr
      password: "${PBR_PASSWORD}"
`)

	config, err := ParseConfig(yamlData)
	if err != nil {
		t.Errorf("Failed to parse config with env var: %s", err)
	}

	if config.Credentials.Git["gitKey"].Token != "exampleToken" {
		t.Errorf("Expected token 'exampleToken', got '%s'", config.Credentials.Git["gitKey"].Token)
	}

	if config.Users["testUser"] != "examplePassword" {
		t.Errorf("Expected user 'testUser' password 'examplePassword', got '%s'", config.Users["testUser"])
	}

	if config.Credentials.ContainerRegistry["cr.platform"].Username != "pbr" {
		t.Errorf("Expected container registry username 'pbr', got '%s'", config.Credentials.ContainerRegistry["cr.platform"].Username)
	}

	if config.Credentials.ContainerRegistry["cr.platform"].Password != "pbrPassword" {
		t.Errorf("Expected container registry password 'pbrPassword', got '%s'", config.Credentials.ContainerRegistry["cr.platform"].Password)
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
