package serve

import (
	"encoding/json"
	"os"
	"path/filepath"

	appanalyze "kops/internal/app/analyze"
)

const cacheFileName = "analysis_cache.json"

// cacheFile is the on-disk cache structure wrapping analysis data with metadata.
type cacheFile struct {
	CachedAt string                  `json:"cached_at"`
	Duration string                  `json:"duration"`
	Data     *appanalyze.AnalysisData `json:"data"`
}

// CacheManager handles reading and writing the local JSON cache file.
type CacheManager struct {
	dir string
}

func NewCacheManager(dir string) *CacheManager {
	return &CacheManager{dir: dir}
}

func (m *CacheManager) filePath() string {
	return filepath.Join(m.dir, cacheFileName)
}

// Exists reports whether a cache file is present.
func (m *CacheManager) Exists() bool {
	_, err := os.Stat(m.filePath())
	return err == nil
}

// Load reads and decodes the cache file. Returns nil on any error.
func (m *CacheManager) Load() (*cacheFile, error) {
	f, err := os.Open(m.filePath())
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cf cacheFile
	if err := json.NewDecoder(f).Decode(&cf); err != nil {
		return nil, err
	}
	return &cf, nil
}

// Save writes the cache file, creating the directory if needed.
func (m *CacheManager) Save(cf *cacheFile) error {
	if err := os.MkdirAll(m.dir, 0755); err != nil {
		return err
	}
	f, err := os.Create(m.filePath())
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(cf)
}
