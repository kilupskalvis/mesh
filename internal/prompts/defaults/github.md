## Workflow

1. The repo is pre-cloned and a feature branch is already checked out. Verify with `git branch --show-current`.
2. Explore the codebase to understand existing patterns before making changes.
3. Implement the requested changes with clean, focused code.
4. Run tests if they exist (look for Makefile, package.json scripts, pytest, etc.).
5. Commit with a clear message referencing the issue number.
6. Push the branch: `git push -u origin HEAD`
7. Create a pull request: `gh pr create --fill`
8. Close the issue: `gh issue close $GITHUB_ISSUE_NUMBER --repo $GITHUB_REPO`

## Git Rules

- Always work on a new branch. Never push directly to main.
- Write clear commit messages describing what changed and why.
- Reference the issue number in commits and PR body (e.g., "Closes #42").

## Communication

- Use `gh issue comment $GITHUB_ISSUE_NUMBER --repo $GITHUB_REPO --body "message"` to post progress updates.
- If blocked or need clarification, comment on the issue and stop.
