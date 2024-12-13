package repository

import (
	"hash/maphash"
	"os"
	"path/filepath"
	"strconv"

	"github.com/hashicorp/golang-lru/v2/simplelru"
)

type Cache struct {
	lru  simplelru.LRUCache[uint64, *Repository]
	path string
}

func NewCache(size int, path string) (*Cache, error) {
	c := &Cache{
		path: path,
	}
	lru, err := simplelru.NewLRU[uint64, *Repository](size, c.onEvict)
	if err != nil {
		return nil, err
	}
	c.lru = lru
	return c, nil
}

func (c *Cache) Get(key string) (*Repository, error) {
	var h maphash.Hash
	// ignoring errors since this never fails
	h.WriteString(key)
	id := h.Sum64()

	if repo, ok := c.lru.Get(id); ok {
		return repo, nil
	}

	hs := strconv.FormatUint(id, 16)
	repoDir := filepath.Join(c.path, hs)
	if err := os.MkdirAll(repoDir, 0700); err != nil {
		return nil, err
	}
	return nil, nil
}

func (c *Cache) onEvict(key uint64, value *Repository) {
}
