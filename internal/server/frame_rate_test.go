package server

import (
	"testing"
	"time"
)

func TestAttachedFrameIntervalSupportedRates(t *testing.T) {
	for _, tc := range []struct {
		value string
		rate  int
	}{
		{"", 30},
		{"30", 30},
		{"60", 60},
		{"120", 120},
	} {
		t.Run(tc.value, func(t *testing.T) {
			if got, want := attachedFrameInterval(tc.value), time.Second/time.Duration(tc.rate); got != want {
				t.Fatalf("interval(%q) = %v, want %v", tc.value, got, want)
			}
		})
	}
}

func TestAttachedFrameIntervalInvalidRatesKeepDefault(t *testing.T) {
	for _, value := range []string{
		"0", "-120", "1", "29", "31", "59", "61", "119", "121", "240",
		"999999999999999999999999", "fast", "120.0", " 120", "120 ", "120\n", "0120", "+120",
	} {
		t.Run(value, func(t *testing.T) {
			if got := attachedFrameInterval(value); got != time.Second/30 {
				t.Fatalf("invalid rate %q produced %v instead of the default", value, got)
			}
		})
	}
}
