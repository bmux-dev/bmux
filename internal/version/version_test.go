package version

import (
	"runtime/debug"
	"testing"
)

func TestResolvePrefersTheReleaseStampThenTheModuleVersion(t *testing.T) {
	module := func(v string) *debug.BuildInfo { return &debug.BuildInfo{Main: debug.Module{Version: v}} }
	for _, c := range []struct {
		name    string
		stamped string
		info    *debug.BuildInfo
		ok      bool
		want    string
	}{
		{"release build", "0.6.0", module("(devel)"), true, "0.6.0"},
		{"stamp with a v", "v0.6.0", nil, false, "0.6.0"},
		{"stamp wins over go install's version", "0.6.0", module("v0.5.0"), true, "0.6.0"},
		{"go install", "", module("v0.6.0"), true, "0.6.0"},
		{"go install of a pre-release tag", "", module("v0.7.0-rc.1"), true, "0.7.0-rc.1"},
		{"checkout build", "", module("(devel)"), true, Dev},
		{"no module version", "", module(""), true, Dev},
		{"no build info", "", nil, false, Dev},
	} {
		if got := resolve(c.stamped, c.info, c.ok); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
