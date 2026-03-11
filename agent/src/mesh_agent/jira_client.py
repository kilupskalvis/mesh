"""Shared Jira HTTP client — proxy handles auth."""

from __future__ import annotations

from typing import Any

import aiohttp


async def jira_get(
    endpoint: str,
    path: str,
    timeout_seconds: int = 10,
) -> tuple[int, dict[str, Any]]:
    """Make a GET request to the Jira API.

    No auth header is sent — the credential proxy injects credentials.

    Returns (status_code, response_json). On non-JSON responses, the dict
    contains {"text": raw_body}.
    """
    url = f"{endpoint}{path}"
    headers: dict[str, str] = {"Accept": "application/json"}

    async with aiohttp.ClientSession() as session:
        async with session.get(
            url, headers=headers, timeout=aiohttp.ClientTimeout(total=timeout_seconds)
        ) as resp:
            try:
                data = await resp.json()
            except Exception:
                data = {"text": await resp.text()}
            return resp.status, data


async def jira_post(
    endpoint: str,
    path: str,
    body: dict[str, Any] | None = None,
    timeout_seconds: int = 15,
) -> tuple[int, dict[str, Any]]:
    """Make a POST request to the Jira API.

    No auth header is sent — the credential proxy injects credentials.

    Returns (status_code, response_json). On non-JSON responses, the dict
    contains {"text": raw_body}.
    """
    url = f"{endpoint}{path}"
    headers: dict[str, str] = {
        "Content-Type": "application/json",
        "Accept": "application/json",
    }

    async with aiohttp.ClientSession() as session:
        async with session.post(
            url, headers=headers, json=body, timeout=aiohttp.ClientTimeout(total=timeout_seconds)
        ) as resp:
            try:
                data = await resp.json()
            except Exception:
                data = {"text": await resp.text()}
            return resp.status, data
