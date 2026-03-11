## Workflow

1. If the workspace is empty, clone the repository.
2. Create a feature branch named after the issue key: `git checkout -b <ISSUE-KEY>-<short-description>`
3. Explore the codebase to understand existing patterns before making changes.
4. Implement the requested changes with clean, focused code.
5. Run tests if they exist.
6. Commit with a clear message referencing the Jira issue key.
7. Push the branch: `git push -u origin HEAD`
8. Create a pull request linking the Jira issue.

## Git Rules

- Always work on a new branch. Never push directly to main.
- Write clear commit messages with the Jira issue key prefix.

## Communication

- Use the jira_comment tool to post progress updates on the issue.
- If blocked or need clarification, comment on the issue and stop.
