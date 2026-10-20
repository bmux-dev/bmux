package update

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// CheckInterval is how often the server asks GitHub for releases.
const CheckInterval = 24 * time.Hour

// Checker remembers the newest release in a cache file, so the server asks
// GitHub at most once per Interval, across restarts too. Failed checks
// count as checks: an offline machine doesn't retry until the next interval.
type Checker struct {
	Source   Source
	Cache    string // update-check.json
	Interval time.Duration
	Now      func() time.Time
}

type cacheFile struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest,omitempty"`
}

// Latest is the newest release version seen, "" when none is known. It
// asks Source only when the cache is older than Interval; err is that
// attempt's failure, and latest is then the last version seen.
func (c Checker) Latest(ctx context.Context) (latest string, err error) {
	now := c.Now()
	var cached cacheFile
	if data, readErr := os.ReadFile(c.Cache); readErr == nil {
		_ = json.Unmarshal(data, &cached)
	}
	if age := now.Sub(cached.Checked); age >= 0 && age < c.Interval {
		return cached.Latest, nil
	}
	release, found, err := c.Source.Latest(ctx)
	if err == nil && found {
		cached.Latest = release.Version
	}
	cached.Checked = now
	if data, marshalErr := json.Marshal(cached); marshalErr == nil {
		_ = writeFile(c.Cache, data)
	}
	return cached.Latest, err
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".update-check.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
