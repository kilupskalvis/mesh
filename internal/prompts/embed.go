package prompts

import (
	_ "embed"
)

//go:embed defaults/github.md
var GitHubDefault string

//go:embed defaults/jira.md
var JiraDefault string

// DefaultForTracker returns the default workflow system prompt for the given tracker kind.
func DefaultForTracker(kind string) string {
	switch kind {
	case "github":
		return GitHubDefault
	case "jira":
		return JiraDefault
	default:
		return ""
	}
}
