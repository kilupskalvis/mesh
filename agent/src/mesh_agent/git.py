"""Git and PR helper module wrapping gh and glab CLI tools."""

from __future__ import annotations

import os
import subprocess
from dataclasses import dataclass


class GitError(Exception):
    """Raised when a git/PR operation fails."""


@dataclass
class PRResult:
    """Result of a PR creation attempt."""

    url: str
    number: int
    provider: str  # "github" or "gitlab"


def create_pull_request(
    title: str,
    body: str,
    base: str = "main",
    head: str | None = None,
    draft: bool = False,
) -> PRResult:
    """Create a pull request using gh or glab, depending on available tokens.

    Uses GITHUB_TOKEN for GitHub (gh) or GITLAB_TOKEN for GitLab (glab).

    Args:
        title: PR title.
        body: PR description body.
        base: Target branch (default: main).
        head: Source branch (default: current branch).
        draft: Whether to create as draft PR.

    Returns:
        PRResult with the PR URL, number, and provider.

    Raises:
        GitError: If PR creation fails or no provider token is available.
    """
    if os.environ.get("GITHUB_TOKEN"):
        return _create_github_pr(title, body, base, head, draft)
    elif os.environ.get("GITLAB_TOKEN"):
        return _create_gitlab_mr(title, body, base, head, draft)
    else:
        raise GitError("Neither GITHUB_TOKEN nor GITLAB_TOKEN is set")


def get_current_branch() -> str:
    """Return the current git branch name.

    Raises:
        GitError: If not in a git repository or git command fails.
    """
    result = subprocess.run(
        ["git", "rev-parse", "--abbrev-ref", "HEAD"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise GitError(f"git rev-parse failed: {result.stderr.strip()}")
    return result.stdout.strip()


def _create_github_pr(
    title: str,
    body: str,
    base: str,
    head: str | None,
    draft: bool,
) -> PRResult:
    """Create a GitHub PR using gh CLI."""
    cmd = ["gh", "pr", "create", "--title", title, "--body", body, "--base", base]
    if head:
        cmd.extend(["--head", head])
    if draft:
        cmd.append("--draft")

    result = subprocess.run(cmd, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise GitError(f"gh pr create failed: {result.stderr.strip()}")

    url = result.stdout.strip()
    number = int(url.rstrip("/").split("/")[-1])
    return PRResult(url=url, number=number, provider="github")


def _create_gitlab_mr(
    title: str,
    body: str,
    base: str,
    head: str | None,
    draft: bool,
) -> PRResult:
    """Create a GitLab MR using glab CLI."""
    cmd = [
        "glab",
        "mr",
        "create",
        "--title",
        title,
        "--description",
        body,
        "--target-branch",
        base,
        "--yes",
    ]
    if head:
        cmd.extend(["--source-branch", head])
    if draft:
        cmd.append("--draft")

    result = subprocess.run(cmd, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        raise GitError(f"glab mr create failed: {result.stderr.strip()}")

    url = result.stdout.strip()
    number = int(url.rstrip("/").split("/")[-1])
    return PRResult(url=url, number=number, provider="gitlab")
