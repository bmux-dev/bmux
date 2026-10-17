package protocol

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

func SessionGeneration(created time.Time) string { return created.Format(time.RFC3339Nano) }

func ValidateSessionName(name string) error {
	if len(name) == 0 || len(name) > 80 {
		return fmt.Errorf("session name must be 1–80 bytes")
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._-", r) {
			return fmt.Errorf("use letters, numbers, dots, dashes or underscores in session names")
		}
	}
	return nil
}
