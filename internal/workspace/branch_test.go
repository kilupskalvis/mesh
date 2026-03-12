package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBranchName_GitHub(t *testing.T) {
	assert.Equal(t, "issue-42-fix-auth-redirect", BranchName("42", "Fix Auth Redirect"))
}

func TestBranchName_TruncatesLongTitle(t *testing.T) {
	long := "This is a very long issue title that should be truncated to forty characters max"
	name := BranchName("7", long)
	assert.LessOrEqual(t, len(name), 60) // issue-7- prefix + 40 slug max
	assert.True(t, name[len(name)-1] != '-', "should not end with dash")
}

func TestBranchName_SpecialCharacters(t *testing.T) {
	assert.Equal(t, "issue-1-fix-bug-in-api-v2", BranchName("1", "Fix bug in API (v2)!"))
}

func TestBranchName_JiraIdentifier(t *testing.T) {
	assert.Equal(t, "issue-PROJ-42-add-search", BranchName("PROJ-42", "Add Search"))
}

func TestBranchName_EmptyTitle(t *testing.T) {
	assert.Equal(t, "issue-42", BranchName("42", ""))
}

func TestBranchName_OnlySpecialChars(t *testing.T) {
	assert.Equal(t, "issue-1", BranchName("1", "!!!"))
}
