package registry

import "gopkg.in/yaml.v3"

type LockDep struct {
	Remote     string `yaml:"remote"`
	Owner      string `yaml:"owner"`
	Repository string `yaml:"repository"`
	Commit     string `yaml:"commit"`
	Digest     string `yaml:"digest"`
}

type BufLock struct {
	Version string    `yaml:"version"`
	Deps    []LockDep `yaml:"deps"`
}

func BufLockFromBytes(data []byte) (*BufLock, error) {
	bufLock := &BufLock{}
	if err := yaml.Unmarshal(data, bufLock); err != nil {
		return nil, err
	}
	return bufLock, nil
}
