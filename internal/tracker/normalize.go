package tracker

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kalvis/mesh/internal/model"
)

// normalizeIssue converts a Jira API issue into the normalized domain model.
func normalizeIssue(raw jiraIssue, endpoint string) model.Issue {
	branchName := fmt.Sprintf("feature/%s", raw.Key)
	issue := model.Issue{
		ID:         raw.ID,
		Identifier: raw.Key,
		Title:      raw.Fields.Summary,
		State:      raw.Fields.Status.Name,
		BranchName: &branchName,
	}

	// Description: ADF -> plain text
	if raw.Fields.Description != nil {
		desc := extractADFText(raw.Fields.Description)
		issue.Description = &desc
	}

	// Priority: numeric ID only
	if raw.Fields.Priority != nil {
		if p, err := strconv.Atoi(raw.Fields.Priority.ID); err == nil {
			issue.Priority = &p
		}
	}

	// URL
	domain := strings.TrimRight(endpoint, "/")
	issueURL := fmt.Sprintf("%s/browse/%s", domain, raw.Key)
	issue.URL = &issueURL

	// Labels: lowercase
	issue.Labels = make([]string, len(raw.Fields.Labels))
	for i, l := range raw.Fields.Labels {
		issue.Labels[i] = strings.ToLower(l)
	}

	// Blockers: inward "Blocks" links
	for _, link := range raw.Fields.IssueLinks {
		if strings.EqualFold(link.Type.Name, "Blocks") && link.InwardIssue != nil {
			ref := model.BlockerRef{
				ID:         &link.InwardIssue.ID,
				Identifier: &link.InwardIssue.Key,
			}
			state := link.InwardIssue.Fields.Status.Name
			ref.State = &state
			issue.BlockedBy = append(issue.BlockedBy, ref)
		}
	}
	if issue.BlockedBy == nil {
		issue.BlockedBy = []model.BlockerRef{}
	}

	// Timestamps
	if t, err := time.Parse(time.RFC3339, raw.Fields.Created); err == nil {
		issue.CreatedAt = &t
	}
	if t, err := time.Parse(time.RFC3339, raw.Fields.Updated); err == nil {
		issue.UpdatedAt = &t
	}

	return issue
}

// extractADFText recursively extracts plain text from Atlassian Document Format JSON.
func extractADFText(node any) string {
	switch v := node.(type) {
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if content, ok := v["content"].([]any); ok {
			var parts []string
			for _, child := range content {
				if t := extractADFText(child); t != "" {
					parts = append(parts, t)
				}
			}
			nodeType, _ := v["type"].(string)
			if nodeType == "paragraph" || nodeType == "heading" {
				return strings.Join(parts, "") + "\n"
			}
			return strings.Join(parts, "")
		}
	case []any:
		var parts []string
		for _, child := range v {
			if t := extractADFText(child); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}
