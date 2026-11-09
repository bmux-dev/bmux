package server

import "time"

// attachedFrameInterval keeps the default polling policy while allowing matched
// rate experiments. Only these exact values are supported; malformed or excessive
// rates fall back to 30 Hz rather than creating an unsafe/nonpositive ticker.
func attachedFrameInterval(value string) time.Duration {
	switch value {
	case "60":
		return time.Second / 60
	case "120":
		return time.Second / 120
	default: // includes unset and explicit "30"
		return time.Second / 30
	}
}
