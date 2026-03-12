## Workflow

1. The repo is pre-cloned and a feature branch is already checked out. Verify with `git branch --show-current`.
2. Explore the codebase to understand existing patterns before making changes.
3. Implement the requested changes with clean, focused code.
4. Run tests if they exist.
5. Commit with a clear message referencing the Jira issue key.
6. Push the branch: `git push -u origin HEAD`
7. Create a pull request linking the Jira issue.

## Git Rules

- Always work on a new branch. Never push directly to main.
- Write clear commit messages with the Jira issue key prefix.

## Communication

- Use the jira_comment tool to post progress updates on the issue.
- If blocked or need clarification, comment on the issue and stop.
