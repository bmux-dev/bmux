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
	Channel  func() string // optional; falls back to Source.Channel
	Cache    string        // update-check.json
	Interval time.Duration
	Now      func() time.Time
}

type cacheFile struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest,omitempty"`
	Channel string    `json:"channel,omitempty"`
}

func normalizeChannel(ch string) string {
	if ch == ChannelBeta {
		return ChannelBeta
	}
	return ChannelStable
}

// Latest is the newest release version seen, "" when none is known. It
// asks Source only when the cache is older than Interval and for the same
// channel; err is that attempt's failure, and latest is then the last version seen.
func (c Checker) Latest(ctx context.Context) (latest string, err error) {
	ch := c.Source.Channel
	if c.Channel != nil {
		ch = c.Channel()
	}
	ch = normalizeChannel(ch)

	now := c.Now()
	var cached cacheFile
	if data, readErr := os.ReadFile(c.Cache); readErr == nil {
		_ = json.Unmarshal(data, &cached)
	}
	if age := now.Sub(cached.Checked); age >= 0 && age < c.Interval && normalizeChannel(cached.Channel) == ch {
		return cached.Latest, nil
	}
	src := c.Source
	src.Channel = ch
	release, found, err := src.Latest(ctx)
	if err == nil {
		cached.Latest = release.Version
		if !found {
			cached.Latest = ""
		}
		cached.Channel = ch
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
