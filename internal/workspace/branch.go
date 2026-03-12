package workspace

import (
	"regexp"
	"strings"
)

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// BranchName generates a git branch name from an issue number and title.
// Format: issue-{number}-{slug} where slug is the title lowercased,
// non-alphanumeric chars replaced with dashes, max 40 chars, no trailing dash.
func BranchName(number, title string) string {
	prefix := "issue-" + number

	slug := strings.ToLower(strings.TrimSpace(title))
	slug = nonAlphanumeric.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")

	if len(slug) > 40 {
		slug = slug[:40]
		slug = strings.TrimRight(slug, "-")
	}

	if slug == "" {
		return prefix
	}
	return prefix + "-" + slug
}
