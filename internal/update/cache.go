package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const checkInterval = 24 * time.Hour

type cacheData struct {
	LastCheck     time.Time `json:"last_check"`
	LatestVersion string    `json:"latest_version"`
}

type UpdateCache struct {
	path string
	data cacheData
}

func NewUpdateCache(path string) *UpdateCache {
	c := &UpdateCache{path: path}
	c.load()
	return c
}

func (c *UpdateCache) ShouldCheck() bool {
	if c.data.LastCheck.IsZero() {
		return true
	}
	return time.Since(c.data.LastCheck) > checkInterval
}

func (c *UpdateCache) LatestVersion() string {
	return c.data.LatestVersion
}

func (c *UpdateCache) Record(version string) {
	c.data.LastCheck = time.Now()
	c.data.LatestVersion = version
	c.save()
}

func (c *UpdateCache) load() {
	f, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	json.Unmarshal(f, &c.data)
}

func (c *UpdateCache) save() {
	f, _ := json.Marshal(c.data)
	os.MkdirAll(filepath.Dir(c.path), 0755)
	os.WriteFile(c.path, f, 0644)
}
