package registry

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/sha3"
)

type manifestEntry struct {
	digest string
	name   string
}

type Manifest struct {
	entry []manifestEntry
}

func (m *Manifest) AddEntry(digest, name string) {
	m.entry = append(m.entry, manifestEntry{digest: digest, name: name})
}

func (m *Manifest) Content() (string, string) {
	var manifestBuilder strings.Builder
	for _, entry := range m.entry {
		manifestBuilder.WriteString(fmt.Sprintf("shake256:%s  %s\n", entry.digest, entry.name))
	}
	manifestContent := manifestBuilder.String()
	h := sha3.NewShake256()
	h.Write([]byte(manifestContent))
	var shake256Sum [64]byte
	h.Read(shake256Sum[:])
	return manifestContent, fmt.Sprintf("%x", shake256Sum)
}
