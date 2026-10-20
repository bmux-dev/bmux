// Package version reports which bmux release this binary is.
package version

import "runtime/debug"

// stamped is set by release builds:
// -ldflags "-X github.com/josephferrero/bmux/internal/version.stamped=0.6.0".
var stamped string

// Dev is the version of a build from a checkout, which isn't a release.
const Dev = "dev"

// String is the version without a leading v, e.g. "0.6.0", or Dev.
func String() string {
	info, ok := debug.ReadBuildInfo()
	return resolve(stamped, info, ok)
}

// resolve prefers the release stamp, then the module version that
// go install records (v0.6.0), and otherwise reports a dev build.
func resolve(stamped string, info *debug.BuildInfo, ok bool) string {
	if stamped != "" {
		return trimV(stamped)
	}
	if ok && info != nil {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return trimV(v)
		}
	}
	return Dev
}

func trimV(v string) string {
	if len(v) > 1 && v[0] == 'v' {
		return v[1:]
	}
	return v
}
