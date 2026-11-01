package observe

import (
	"os"
	"sync"
	"time"
)

// manifestCache remembers what a manifest said.
//
// A manifest is written once, at the seal, and never changes afterwards; a rewrite of the same step
// produces a new file with a new modification time. That makes (path, mtime, size) an exact key: a
// hit is not a heuristic, it is the same bytes.
//
// The cache saves the read, not the stat: the stat is still needed for the "is this shard newer than
// the seal" rule, and a stat is the cheaper of the two.
type manifestCache struct {
	mu sync.Mutex
	m  map[string]cacheEntry
}

type cacheEntry struct {
	modTime time.Time
	size    int64
	parsed  manifest
}

func newManifestCache() *manifestCache {
	return &manifestCache{m: map[string]cacheEntry{}}
}

// get returns the parsed manifest when the file on disk is the one that was parsed.
func (c *manifestCache) get(path string, info os.FileInfo) (manifest, bool) {
	if c == nil || info == nil {
		return manifest{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[path]
	if !ok || e.size != info.Size() || !e.modTime.Equal(info.ModTime()) {
		return manifest{}, false
	}
	return e.parsed, true
}

// prune drops entries for directories that no longer exist. Without it the cache outlives the
// checkpoints it describes, and a controller that runs for a week holds every manifest it ever saw.
func (c *manifestCache) prune(alive map[string]bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for path := range c.m {
		if !alive[path] {
			delete(c.m, path)
		}
	}
}

// put records one parsed manifest. A nil cache (the test path) does nothing.
func (c *manifestCache) put(path string, info os.FileInfo, m manifest) {
	if c == nil || info == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[path] = cacheEntry{modTime: info.ModTime(), size: info.Size(), parsed: m}
}
