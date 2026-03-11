"""Tests for SDK configuration building."""

from __future__ import annotations

from unittest.mock import patch, MagicMock

from mesh_agent.sdk_config import build_mcp_server, build_plugins


def test_build_mcp_server_returns_server() -> None:
    with patch("mesh_agent.sdk_config.create_sdk_mcp_server", return_value={"mock": True}) as mock:
        result = build_mcp_server()
        mock.assert_called_once()
        assert result == {"mock": True}


def test_build_plugins_empty_when_no_dirs() -> None:
    with patch("os.path.isdir", return_value=False):
        assert build_plugins() == []


def test_build_plugins_finds_existing_dirs() -> None:
    def fake_isdir(path: str) -> bool:
        return "pyright-lsp" in path

    with patch("os.path.isdir", side_effect=fake_isdir):
        plugins = build_plugins()
        assert len(plugins) == 1
        assert "pyright-lsp" in plugins[0]["path"]
