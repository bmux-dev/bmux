package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewerFollowsSemverPrecedence(t *testing.T) {
	for _, c := range []struct {
		a, b  string
		newer bool
	}{
		{"0.6.0", "0.5.0", true},
		{"0.10.0", "0.9.0", true},
		{"1.0.0", "0.99.99", true},
		{"0.5.1", "0.5.0", true},
		{"0.6.0", "0.6.0-rc.1", true},
		{"0.6.0-rc.2", "0.6.0-rc.1", true},
		{"0.6.0-rc.10", "0.6.0-rc.9", true},
		{"0.6.0-rc.1", "0.5.9", true},
		{"0.6.0-rc.1.1", "0.6.0-rc.1", true},
		{"0.6.0-beta", "0.6.0-alpha", true},
		{"0.6.0-alpha", "0.6.0-1", true},
		{"0.6.0", "0.6.0", false},
		{"0.6.0+build.2", "0.6.0+build.1", false},
		{"0.5.0", "0.6.0", false},
		{"0.6.0-rc.1", "0.6.0", false},
		{"dev", "0.5.0", false},
		{"0.6.0", "dev", false},
		{"0.6", "0.5.0", false},
		{"v0.6.0", "0.5.0", false},
		{"0.06.0", "0.5.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.newer {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.newer)
		}
	}
}

// fakeReleases serves a GitHub-style release list and its assets over TLS.
type fakeReleases struct {
	server   *httptest.Server
	list     func(base string) string
	files    map[string][]byte
	requests atomic.Int32
}

func newFakeReleases(t *testing.T) *fakeReleases {
	f := &fakeReleases{files: map[string][]byte{}}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		if r.URL.Path == "/releases" {
			fmt.Fprint(w, f.list(f.server.URL))
			return
		}
		data, ok := f.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeReleases) source() Source {
	return Source{Releases: f.server.URL + "/releases", HTTP: f.server.Client(), OS: "linux", Arch: "amd64"}
}

func archiveWith(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sumLine(data []byte, name string) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) + "  " + name + "\n"
}

func releaseJSON(tag string, draft bool, assets ...string) map[string]any {
	var list []map[string]string
	for _, a := range assets {
		list = append(list, map[string]string{"name": a, "browser_download_url": "BASE/" + tag + "/" + a})
	}
	return map[string]any{"tag_name": tag, "draft": draft, "prerelease": true, "assets": list}
}

func listOf(releases ...map[string]any) func(string) string {
	data, _ := json.Marshal(releases)
	return func(base string) string { return strings.ReplaceAll(string(data), "BASE", base) }
}

func TestLatestPicksTheHighestReleaseWithThisPlatformsFiles(t *testing.T) {
	f := newFakeReleases(t)
	full := []string{"bmux_linux_amd64.tar.gz", "bmux_darwin_arm64.tar.gz", "checksums.txt"}
	f.list = listOf(
		releaseJSON("v0.9.0", true, full...),                                      // draft
		releaseJSON("v0.8.0", false, "bmux_darwin_arm64.tar.gz", "checksums.txt"), // no linux/amd64
		releaseJSON("v0.7.0", false, "bmux_linux_amd64.tar.gz"),                   // no checksums
		releaseJSON("nightly", false, full...),                                    // not a version
		releaseJSON("v0.6.0-rc.1", false, full...),
		releaseJSON("v0.6.0", false, full...), // listed after its rc: order doesn't matter
		releaseJSON("v0.5.0", false, full...),
	)
	got, found, err := f.source().Latest(context.Background())
	if err != nil || !found {
		t.Fatalf("Latest: %v %v", found, err)
	}
	want := Release{Version: "0.6.0", Tag: "v0.6.0", Asset: "bmux_linux_amd64.tar.gz",
		ArchiveURL: f.server.URL + "/v0.6.0/bmux_linux_amd64.tar.gz", ChecksumsURL: f.server.URL + "/v0.6.0/checksums.txt"}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}

	f.list = listOf(releaseJSON("v0.5.0", false, "bmux_darwin_arm64.tar.gz", "checksums.txt"))
	if _, found, err := f.source().Latest(context.Background()); found || err != nil {
		t.Fatalf("a release without this platform's archive was offered: %v %v", found, err)
	}
}

func installFixture(t *testing.T, archive []byte, sums string) (Source, Release, string) {
	t.Helper()
	f := newFakeReleases(t)
	f.files["/v0.6.0/bmux_linux_amd64.tar.gz"] = archive
	f.files["/v0.6.0/checksums.txt"] = []byte(sums)
	release := Release{Version: "0.6.0", Tag: "v0.6.0", Asset: "bmux_linux_amd64.tar.gz",
		ArchiveURL: f.server.URL + "/v0.6.0/bmux_linux_amd64.tar.gz", ChecksumsURL: f.server.URL + "/v0.6.0/checksums.txt"}
	exe := filepath.Join(t.TempDir(), "bmux")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return f.source(), release, exe
}

func TestInstallReplacesTheBinaryOnlyWhenTheChecksumMatches(t *testing.T) {
	archive := archiveWith(t, map[string]string{"LICENSE": "MIT", "bmux": "new binary"})
	other := sumLine([]byte("x"), "bmux_darwin_arm64.tar.gz")

	source, release, exe := installFixture(t, archive, other+sumLine(archive, "bmux_linux_amd64.tar.gz"))
	if err := source.Install(context.Background(), release, exe); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(exe)
	info, _ := os.Stat(exe)
	if string(data) != "new binary" || info.Mode().Perm() != 0o755 {
		t.Fatalf("installed %q with mode %v", data, info.Mode())
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".bmux.update.*")); len(leftovers) != 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}

	for name, c := range map[string]struct {
		archive []byte
		sums    string
		err     string
	}{
		"tampered archive":       {append(append([]byte{}, archive...), 'x'), sumLine(archive, "bmux_linux_amd64.tar.gz"), "checksum mismatch"},
		"no entry":               {archive, other, "no entry for bmux_linux_amd64.tar.gz"},
		"no bmux in the archive": {archiveWith(t, map[string]string{"README.md": "hi"}), "", "no bmux binary"},
	} {
		sums := c.sums
		if sums == "" {
			sums = sumLine(c.archive, "bmux_linux_amd64.tar.gz")
		}
		source, release, exe := installFixture(t, c.archive, sums)
		err := source.Install(context.Background(), release, exe)
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: error %v, want %q", name, err, c.err)
		}
		if data, _ := os.ReadFile(exe); string(data) != "old binary" {
			t.Errorf("%s: the old binary was replaced with %q", name, data)
		}
	}
}

func TestDownloadsRefusePlainHTTP(t *testing.T) {
	source := GitHub()
	source.Releases = "http://example.com/releases"
	if _, _, err := source.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "plain HTTP") {
		t.Fatalf("listed releases over HTTP: %v", err)
	}
	exe := filepath.Join(t.TempDir(), "bmux")
	_ = os.WriteFile(exe, []byte("old binary"), 0o755)
	err := source.Install(context.Background(), Release{Asset: "bmux_linux_amd64.tar.gz", ChecksumsURL: "http://example.com/checksums.txt", ArchiveURL: "https://example.com/a"}, exe)
	if err == nil || !strings.Contains(err.Error(), "plain HTTP") {
		t.Fatalf("downloaded checksums over HTTP: %v", err)
	}
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/elsewhere", http.StatusFound)
	}))
	defer redirect.Close()
	client := redirect.Client()
	source = Source{Releases: redirect.URL + "/releases", HTTP: httpsOnly(client), OS: "linux", Arch: "amd64"}
	if _, _, err := source.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("followed a redirect to HTTP: %v", err)
	}
}

func TestCheckerAsksAtMostOncePerInterval(t *testing.T) {
	f := newFakeReleases(t)
	full := []string{"bmux_linux_amd64.tar.gz", "checksums.txt"}
	f.list = listOf(releaseJSON("v0.6.0", false, full...))
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	checker := Checker{Source: f.source(), Cache: filepath.Join(t.TempDir(), "state", "update-check.json"), Interval: CheckInterval, Now: func() time.Time { return now }}

	if latest, err := checker.Latest(context.Background()); latest != "0.6.0" || err != nil || f.requests.Load() != 1 {
		t.Fatalf("first check: %q %v, %d requests", latest, err, f.requests.Load())
	}
	// A restart within the interval reads the cache.
	now = now.Add(23 * time.Hour)
	f.list = listOf(releaseJSON("v0.7.0", false, full...))
	if latest, err := checker.Latest(context.Background()); latest != "0.6.0" || err != nil || f.requests.Load() != 1 {
		t.Fatalf("cached check: %q %v, %d requests", latest, err, f.requests.Load())
	}
	now = now.Add(time.Hour)
	if latest, _ := checker.Latest(context.Background()); latest != "0.7.0" || f.requests.Load() != 2 {
		t.Fatalf("after the interval: %q, %d requests", latest, f.requests.Load())
	}
	// A failure keeps the last version and still waits a full interval.
	f.list = func(string) string { return "not json" }
	now = now.Add(CheckInterval)
	if latest, err := checker.Latest(context.Background()); latest != "0.7.0" || err == nil {
		t.Fatalf("failed check: %q %v", latest, err)
	}
	now = now.Add(time.Hour)
	if _, err := checker.Latest(context.Background()); err != nil || f.requests.Load() != 3 {
		t.Fatalf("retried before the interval: %v, %d requests", err, f.requests.Load())
	}
	// A clock set back doesn't freeze checks forever.
	now = now.Add(-48 * time.Hour)
	f.list = listOf(releaseJSON("v0.7.0", false, full...))
	if _, err := checker.Latest(context.Background()); err != nil || f.requests.Load() != 4 {
		t.Fatalf("clock set back: %v, %d requests", err, f.requests.Load())
	}
}
