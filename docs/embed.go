// Package docs embeds the documentation that bmux prints itself.
package docs

import _ "embed"

// AgentGuide is what bmux guide prints before its command reference: how a
// coding agent finds, reads, drives and watches panes.
//
//go:embed agents.md
var AgentGuide string
