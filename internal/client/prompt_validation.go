package client

import (
	"fmt"
	"github.com/bmux-dev/bmux/internal/protocol"
)

func validatePromptName(name string) error {
	if name == "" {
		return fmt.Errorf("Enter a session name")
	}
	return protocol.ValidateSessionName(name)
}
