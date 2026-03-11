## Workflow

1. If the workspace is empty, clone the repo: `gh repo clone $GITHUB_REPO .`
2. Create a feature branch: `git checkout -b issue-<number>-<short-description>`
3. Explore the codebase to understand existing patterns before making changes.
4. Implement the requested changes with clean, focused code.
5. Run tests if they exist (look for Makefile, package.json scripts, pytest, etc.).
6. Commit with a clear message referencing the issue number.
7. Push the branch: `git push -u origin HEAD`
8. Create a pull request: `gh pr create --fill`
9. Close the issue: `gh issue close $GITHUB_ISSUE_NUMBER --repo $GITHUB_REPO`

## Git Rules

- Always work on a new branch. Never push directly to main.
- Write clear commit messages describing what changed and why.
- Reference the issue number in commits and PR body (e.g., "Closes #42").

## Communication

- Use `gh issue comment $GITHUB_ISSUE_NUMBER --repo $GITHUB_REPO --body "message"` to post progress updates.
- If blocked or need clarification, comment on the issue and stop.
