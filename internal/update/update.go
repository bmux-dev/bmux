// Package update finds bmux releases on GitHub and installs them: the daily
// check behind the status bar's hint, and bmux update.
//
// A release counts only if it has this platform's archive and checksums.txt,
// the files GoReleaser publishes (.goreleaser.yaml). Installs verify the
// archive's SHA-256 against checksums.txt before anything is replaced, and the
// binary is swapped with one rename, so it is always either the old or the
// new one.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Repository is where releases are published.
const Repository = "bmux-dev/bmux"

const (
	maxListBytes      = 4 << 20
	maxChecksumsBytes = 64 << 10
	maxArchiveBytes   = 256 << 20
	maxBinaryBytes    = 256 << 20
)

// Release is a published release with an archive for this platform.
type Release struct {
	Version      string // "0.6.0"
	Tag          string // "v0.6.0"
	Asset        string // "bmux_linux_amd64.tar.gz"
	ArchiveURL   string
	ChecksumsURL string
}

// Source lists and downloads releases.
type Source struct {
	// Releases is the GitHub API's releases list for the repository.
	Releases string
	HTTP     *http.Client
	OS, Arch string
}

// GitHub is the source bmux uses: the repository's releases on GitHub, for
// the platform this binary was built for.
func GitHub() Source {
	return Source{
		Releases: "https://api.github.com/repos/" + Repository + "/releases?per_page=30",
		HTTP:     httpsOnly(&http.Client{Timeout: 2 * time.Minute}),
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
	}
}

// httpsOnly refuses redirects away from HTTPS; GitHub's downloads redirect
// to its object storage.
func httpsOnly(c *http.Client) *http.Client {
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme != "https" {
			return fmt.Errorf("refusing to follow a redirect to %s", r.URL.Redacted())
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}
	return c
}

// AssetName is the archive GoReleaser builds for this platform.
func (s Source) AssetName() string { return "bmux_" + s.OS + "_" + s.Arch + ".tar.gz" }

type githubRelease struct {
	TagName string `json:"tag_name"`
	Draft   bool   `json:"draft"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest is the highest-versioned release with this platform's archive and
// checksums. Pre-releases count: every 0.x release is one. Drafts don't.
func (s Source) Latest(ctx context.Context) (Release, bool, error) {
	body, err := s.get(ctx, s.Releases, maxListBytes)
	if err != nil {
		return Release{}, false, err
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return Release{}, false, fmt.Errorf("reading the release list: %w", err)
	}
	var best Release
	found := false
	for _, r := range releases {
		v, ok := strings.CutPrefix(r.TagName, "v")
		if r.Draft || !ok || !Valid(v) {
			continue
		}
		candidate := Release{Version: v, Tag: r.TagName, Asset: s.AssetName()}
		for _, a := range r.Assets {
			switch a.Name {
			case candidate.Asset:
				candidate.ArchiveURL = a.URL
			case "checksums.txt":
				candidate.ChecksumsURL = a.URL
			}
		}
		if candidate.ArchiveURL == "" || candidate.ChecksumsURL == "" {
			continue
		}
		if !found || Newer(v, best.Version) {
			best, found = candidate, true
		}
	}
	return best, found, nil
}

// Install downloads r, checks it against its checksums.txt and replaces the
// executable at path with the bmux inside. Nothing at path changes unless
// every step succeeds.
func (s Source) Install(ctx context.Context, r Release, path string) error {
	sums, err := s.get(ctx, r.ChecksumsURL, maxChecksumsBytes)
	if err != nil {
		return err
	}
	want, err := checksumFor(sums, r.Asset)
	if err != nil {
		return err
	}
	archive, err := s.get(ctx, r.ArchiveURL, maxArchiveBytes)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", r.Asset, want, got)
	}
	return replace(path, archive)
}

func (s Source) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("refusing to download over plain HTTP: %s", url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", url, limit)
	}
	return body, nil
}

// checksumFor finds name's SHA-256 in a sha256sum-format file, as
// GoReleaser writes it.
func checksumFor(sums []byte, name string) (string, error) {
	lines := bufio.NewScanner(bytes.NewReader(sums))
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			if len(fields[0]) != sha256.Size*2 {
				break
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

// replace unpacks the archive's bmux next to path and renames it over path.
func replace(path string, archive []byte) error {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("unpacking the archive: %w", err)
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("the archive has no bmux binary")
		}
		if err != nil {
			return fmt.Errorf("unpacking the archive: %w", err)
		}
		if h.Name != "bmux" || h.Typeflag != tar.TypeReg {
			continue
		}
		if h.Size > maxBinaryBytes {
			return fmt.Errorf("the archive's bmux is larger than %d bytes", maxBinaryBytes)
		}
		return swap(path, tr)
	}
}

func swap(path string, binary io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".bmux.update.*")
	if err != nil {
		return fmt.Errorf("can't write next to %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if _, err = io.Copy(tmp, io.LimitReader(binary, maxBinaryBytes)); err == nil {
		err = tmp.Chmod(0o755)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// Same directory, so the rename is atomic; a running bmux keeps its file.
	return os.Rename(tmp.Name(), path)
}
