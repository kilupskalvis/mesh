"""Tests for git.py — PR creation and branch helpers."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pytest

from mesh_agent.git import GitError, create_pull_request, get_current_branch


def test_create_pr_uses_github_when_token_set(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("GITHUB_TOKEN", "ghp_test")
    monkeypatch.delenv("GITLAB_TOKEN", raising=False)
    with patch("mesh_agent.git.subprocess.run") as mock_run:
        mock_run.return_value = MagicMock(
            returncode=0, stdout="https://github.com/org/repo/pull/42\n"
        )
        result = create_pull_request("Fix bug", "Description", base="main")
        assert result.provider == "github"
        assert result.number == 42
        assert result.url == "https://github.com/org/repo/pull/42"
        cmd = mock_run.call_args[0][0]
        assert cmd[:3] == ["gh", "pr", "create"]


def test_create_pr_uses_gitlab_when_token_set(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("GITHUB_TOKEN", raising=False)
    monkeypatch.setenv("GITLAB_TOKEN", "glpat_test")
    with patch("mesh_agent.git.subprocess.run") as mock_run:
        mock_run.return_value = MagicMock(
            returncode=0, stdout="https://gitlab.com/org/repo/-/merge_requests/7\n"
        )
        result = create_pull_request("Fix bug", "Description")
        assert result.provider == "gitlab"
        assert result.number == 7


def test_create_pr_raises_when_no_token(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("GITHUB_TOKEN", raising=False)
    monkeypatch.delenv("GITLAB_TOKEN", raising=False)
    with pytest.raises(GitError, match="Neither GITHUB_TOKEN nor GITLAB_TOKEN"):
        create_pull_request("title", "body")


def test_create_pr_with_head_and_draft(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("GITHUB_TOKEN", "ghp_test")
    with patch("mesh_agent.git.subprocess.run") as mock_run:
        mock_run.return_value = MagicMock(
            returncode=0, stdout="https://github.com/org/repo/pull/10\n"
        )
        create_pull_request("Title", "Body", base="develop", head="feature/x", draft=True)
        cmd = mock_run.call_args[0][0]
        assert "--head" in cmd
        assert "feature/x" in cmd
        assert "--draft" in cmd
        assert "--base" in cmd
        assert "develop" in cmd


def test_create_pr_github_failure(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("GITHUB_TOKEN", "ghp_test")
    with patch("mesh_agent.git.subprocess.run") as mock_run:
        mock_run.return_value = MagicMock(returncode=1, stderr="auth failed\n")
        with pytest.raises(GitError, match="gh pr create failed"):
            create_pull_request("title", "body")


def test_get_current_branch() -> None:
    with patch("mesh_agent.git.subprocess.run") as mock_run:
        mock_run.return_value = MagicMock(returncode=0, stdout="feature/PROJ-123\n")
        assert get_current_branch() == "feature/PROJ-123"


def test_get_current_branch_failure() -> None:
    with patch("mesh_agent.git.subprocess.run") as mock_run:
        mock_run.return_value = MagicMock(returncode=128, stderr="not a git repo\n")
        with pytest.raises(GitError, match="git rev-parse failed"):
            get_current_branch()
