package update

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCacheShouldCheck_NoCacheFile(t *testing.T) {
	dir := t.TempDir()
	c := NewUpdateCache(filepath.Join(dir, "update-check.json"))
	if !c.ShouldCheck() {
		t.Fatal("expected ShouldCheck=true when no cache file exists")
	}
}

func TestCacheShouldCheck_RecentCheck(t *testing.T) {
	dir := t.TempDir()
	c := NewUpdateCache(filepath.Join(dir, "update-check.json"))
	c.Record("0.1.3")
	if c.ShouldCheck() {
		t.Fatal("expected ShouldCheck=false immediately after Record")
	}
}

func TestCacheShouldCheck_StaleCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "update-check.json")
	c := NewUpdateCache(path)
	c.Record("0.1.3")
	c.data.LastCheck = time.Now().Add(-25 * time.Hour)
	c.save()
	c2 := NewUpdateCache(path)
	if !c2.ShouldCheck() {
		t.Fatal("expected ShouldCheck=true after 25h")
	}
}

func TestCacheLatestVersion(t *testing.T) {
	dir := t.TempDir()
	c := NewUpdateCache(filepath.Join(dir, "update-check.json"))
	c.Record("0.1.3")
	if v := c.LatestVersion(); v != "0.1.3" {
		t.Fatalf("expected 0.1.3, got %s", v)
	}
}
