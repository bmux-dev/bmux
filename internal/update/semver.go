package update

import (
	"strconv"
	"strings"
)

// Valid reports whether v is a semantic version such as 0.6.0 or
// 0.7.0-rc.1, without a leading v.
func Valid(v string) bool {
	_, ok := parse(v)
	return ok
}

// Newer reports whether version a comes after b by semver precedence:
// 0.6.0 > 0.6.0-rc.2 > 0.6.0-rc.1 > 0.5.1. Build metadata (+...) is ignored.
// Invalid versions are never newer.
func Newer(a, b string) bool {
	va, okA := parse(a)
	vb, okB := parse(b)
	return okA && okB && compare(va, vb) > 0
}

type semver struct {
	core [3]uint64
	pre  []string
}

func parse(v string) (semver, bool) {
	v, _, _ = strings.Cut(v, "+")
	core, pre, hasPre := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 || (hasPre && pre == "") {
		return semver{}, false
	}
	var s semver
	for i, p := range parts {
		n, ok := number(p)
		if !ok {
			return semver{}, false
		}
		s.core[i] = n
	}
	if hasPre {
		s.pre = strings.Split(pre, ".")
		for _, id := range s.pre {
			if id == "" || strings.Trim(id, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-") != "" {
				return semver{}, false
			}
		}
	}
	return s, true
}

// number parses a numeric identifier: digits, no leading zero.
func number(s string) (uint64, bool) {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

func compare(a, b semver) int {
	for i := range a.core {
		if a.core[i] != b.core[i] {
			if a.core[i] > b.core[i] {
				return 1
			}
			return -1
		}
	}
	// A release outranks its pre-releases.
	switch {
	case len(a.pre) == 0 && len(b.pre) == 0:
		return 0
	case len(a.pre) == 0:
		return 1
	case len(b.pre) == 0:
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		if c := compareIdentifier(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a.pre) > len(b.pre):
		return 1
	case len(a.pre) < len(b.pre):
		return -1
	}
	return 0
}

// compareIdentifier orders numeric identifiers numerically and below
// alphanumeric ones, which compare as ASCII.
func compareIdentifier(a, b string) int {
	na, aNum := number(a)
	nb, bNum := number(b)
	switch {
	case aNum && bNum:
		if na == nb {
			return 0
		}
		if na > nb {
			return 1
		}
		return -1
	case aNum:
		return -1
	case bNum:
		return 1
	}
	return strings.Compare(a, b)
}
