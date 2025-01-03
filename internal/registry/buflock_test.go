package registry

import "testing"

func TestBufLockYaml(t *testing.T) {
	yamlStr := `# Generated by buf. DO NOT EDIT.
version: v1
deps:
  - remote: pbr-example.greatlion.tech
    owner: googleapis
    repository: googleapis
    commit: d55dd1daa4a6f127379ea752e405fac9
    digest: shake256:a85b30beec1bd633f7a5dcbfea110cef65b927a1af836fd2bfb58880812d912497e17e7a508de9e32bdeb2a571582a4e1d83182bdd5aa5f8732792719a164380
  - remote: pbr-example.greatlion.tech
    owner: greatliontech
    repository: private-test-protos
    commit: f1051bea88452da60c7fbcfc4cf5e71b
    digest: shake256:b4059131b3adb8e893ccb639c61de712491b695f627b3b5ed596f6527e7f0289fc14f4429ffb26feda32ccd25ba6d431547d61c13d023d20fdbb9b5d8b5d5d19
  - remote: pbr-example.greatlion.tech
    owner: greatliontech
    repository: protoc-gen-rtk-query
    commit: daa72aff329eceefabdbffe432f1b4b6
    digest: shake256:b186c9ccde4e247d227824625ef58be7d56c84bd7f55f0b00e744f68223f1a0a57b0c20198d4f4548539102bcb4e17102cbda51de18adc5051402b6dae58daf4
`
	bl, err := BufLockFromBytes([]byte(yamlStr))
	if err != nil {
		t.Fatal(err)
	}

	if bl.Version != "v1" {
		t.Fatalf("expected version v1, got %s", bl.Version)
	}

	expectedDeps := []LockDep{
		{
			Remote:     "pbr-example.greatlion.tech",
			Owner:      "googleapis",
			Repository: "googleapis",
			Commit:     "d55dd1daa4a6f127379ea752e405fac9",
			Digest:     "shake256:a85b30beec1bd633f7a5dcbfea110cef65b927a1af836fd2bfb58880812d912497e17e7a508de9e32bdeb2a571582a4e1d83182bdd5aa5f8732792719a164380",
		},
		{
			Remote:     "pbr-example.greatlion.tech",
			Owner:      "greatliontech",
			Repository: "private-test-protos",
			Commit:     "f1051bea88452da60c7fbcfc4cf5e71b",
			Digest:     "shake256:b4059131b3adb8e893ccb639c61de712491b695f627b3b5ed596f6527e7f0289fc14f4429ffb26feda32ccd25ba6d431547d61c13d023d20fdbb9b5d8b5d5d19",
		},
		{
			Remote:     "pbr-example.greatlion.tech",
			Owner:      "greatliontech",
			Repository: "protoc-gen-rtk-query",
			Commit:     "daa72aff329eceefabdbffe432f1b4b6",
			Digest:     "shake256:b186c9ccde4e247d227824625ef58be7d56c84bd7f55f0b00e744f68223f1a0a57b0c20198d4f4548539102bcb4e17102cbda51de18adc5051402b6dae58daf4",
		},
	}

	for i, dep := range bl.Deps {
		if dep.Remote != expectedDeps[i].Remote {
			t.Fatalf("expected remote %s, got %s", expectedDeps[i].Remote, dep.Remote)
		}
		if dep.Owner != expectedDeps[i].Owner {
			t.Fatalf("expected owner %s, got %s", expectedDeps[i].Owner, dep.Owner)
		}
		if dep.Repository != expectedDeps[i].Repository {
			t.Fatalf("expected repository %s, got %s", expectedDeps[i].Repository, dep.Repository)
		}
		if dep.Commit != expectedDeps[i].Commit {
			t.Fatalf("expected commit %s, got %s", expectedDeps[i].Commit, dep.Commit)
		}
		if dep.Digest != expectedDeps[i].Digest {
			t.Fatalf("expected digest %s, got %s", expectedDeps[i].Digest, dep.Digest)
		}
	}
}
