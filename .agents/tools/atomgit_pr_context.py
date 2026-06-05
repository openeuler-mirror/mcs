#!/usr/bin/env python3
"""Fetch AtomGit pull request context and submit maintainer-approved comments.

Context fetching is read-only: it does not checkout branches, modify the
worktree, or submit review comments. Comment submission is a separate explicit
mode intended only after maintainer confirmation.
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import re
import sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.parse import quote, urlparse

try:
    import requests
except ImportError as exc:  # pragma: no cover - environment guard
    raise SystemExit("requests is required to use this tool") from exc


DEFAULT_OWNER = "openEuler"
DEFAULT_REPO = "mcs"
DEFAULT_BASE_URL = "https://api.atomgit.com"


class AtomGitError(RuntimeError):
    """Raised when the AtomGit API returns an unexpected response."""


def parse_pr_url(url: str | None) -> dict[str, Any]:
    """Parse AtomGit PR URL into owner, repo, and pr number when present."""
    if not url:
        return {}

    parsed = urlparse(url)
    parts = [part for part in parsed.path.split("/") if part]
    if len(parts) < 4:
        return {}

    pr_number = None
    for marker in ("pull", "pulls"):
        if marker in parts:
            index = parts.index(marker)
            if index + 1 < len(parts) and parts[index + 1].isdigit():
                pr_number = int(parts[index + 1])
                break

    if pr_number is None:
        return {}

    return {
        "owner": parts[0],
        "repo": parts[1],
        "pr": pr_number,
    }


def normalize_patch(patch: Any) -> str:
    """Normalize AtomGit patch field, which may be a string or object."""
    if isinstance(patch, dict):
        return str(patch.get("diff") or patch.get("patch") or "")
    if patch is None:
        return ""
    return str(patch)


def add_line_numbers(content: str) -> str:
    """Return content with stable 1-based line numbers for review."""
    return "\n".join(f"{idx}: {line}" for idx, line in enumerate(content.splitlines(), start=1))


def decode_content(data: dict[str, Any]) -> str:
    """Decode AtomGit contents API payload."""
    content = data.get("content") or ""
    if not content:
        return ""

    encoding = data.get("encoding", "base64")
    if encoding != "base64":
        return str(content)

    cleaned = re.sub(r"\s+", "", str(content))
    return base64.b64decode(cleaned).decode("utf-8", "replace")


class AtomGitClient:
    """Minimal AtomGit API client for PR review workflows."""

    def __init__(self, token: str, owner: str, repo: str, base_url: str) -> None:
        self.token = token
        self.owner = owner
        self.repo = repo
        self.base_url = base_url.rstrip("/")
        self.session = requests.Session()
        self.session.headers.update(
            {
                "Authorization": f"Bearer {token}",
                "Content-Type": "application/json",
                "User-Agent": "mcs-agent/atomgit-pr-context",
            }
        )

    @property
    def repo_path(self) -> str:
        owner = quote(self.owner, safe="")
        repo = quote(self.repo, safe="")
        return f"/api/v5/repos/{owner}/{repo}"

    def request(
        self,
        method: str,
        endpoint: str,
        *,
        params: dict[str, Any] | None = None,
        body: dict[str, Any] | None = None,
    ) -> Any:
        url = f"{self.base_url}{endpoint}"
        response = self.session.request(method=method, url=url, json=body, params=params, timeout=30)
        if response.status_code in (200, 201, 202):
            try:
                return response.json()
            except ValueError:
                return {"data": response.text}
        if response.status_code == 204:
            return {}

        raise AtomGitError(f"{method} {endpoint} failed: HTTP {response.status_code}: {response.text}")

    def get_pull_request(self, pr_number: int) -> dict[str, Any]:
        return self.request("GET", f"{self.repo_path}/pulls/{pr_number}")

    def get_pr_files(self, pr_number: int) -> list[dict[str, Any]]:
        return self.request("GET", f"{self.repo_path}/pulls/{pr_number}/files")

    def get_pr_commits(self, pr_number: int) -> list[dict[str, Any]]:
        return self.request("GET", f"{self.repo_path}/pulls/{pr_number}/commits")

    def get_pr_comments(self, pr_number: int) -> list[dict[str, Any]]:
        return self.request("GET", f"{self.repo_path}/pulls/{pr_number}/comments")

    def submit_pr_comment(self, pr_number: int, body: str) -> dict[str, Any]:
        return self.request("POST", f"{self.repo_path}/pulls/{pr_number}/comments", body={"body": body})

    def get_file_content(self, file_path: str, ref: str) -> str:
        encoded_path = quote(file_path, safe="")
        data = self.request("GET", f"{self.repo_path}/contents/{encoded_path}", params={"ref": ref})
        return decode_content(data)


def build_pr_context(client: AtomGitClient, pr_number: int, *, include_comments: bool) -> dict[str, Any]:
    pr = client.get_pull_request(pr_number)
    files = client.get_pr_files(pr_number)
    commits = client.get_pr_commits(pr_number)
    comments = client.get_pr_comments(pr_number) if include_comments else []

    head = pr.get("head") or {}
    base = pr.get("base") or {}
    user = pr.get("user") or {}
    head_sha = head.get("sha") or head.get("commit_sha") or "HEAD"

    changed_files: list[dict[str, Any]] = []
    for file_info in files:
        filename = file_info.get("filename") or file_info.get("file_name") or file_info.get("path")
        status = file_info.get("status") or "modified"
        item: dict[str, Any] = {
            "filename": filename,
            "status": status,
            "additions": file_info.get("additions", 0),
            "deletions": file_info.get("deletions", 0),
            "patch": normalize_patch(file_info.get("patch")),
        }

        if filename and status != "removed":
            try:
                content = client.get_file_content(filename, head_sha)
                item["content"] = add_line_numbers(content)
            except Exception as exc:  # noqa: BLE001 - keep per-file extraction best-effort
                item["content"] = f"# Error fetching content: {exc}"

        changed_files.append(item)

    inline_comments = sum(1 for comment in comments if comment.get("path") or comment.get("diff_file"))
    unresolved_comments = sum(1 for comment in comments if not comment.get("resolved_at"))
    additions = sum(int(file_info.get("additions") or 0) for file_info in files)
    deletions = sum(int(file_info.get("deletions") or 0) for file_info in files)

    return {
        "fetch_time": datetime.now(timezone.utc).isoformat(),
        "source": {
            "platform": "atomgit",
            "owner": client.owner,
            "repo": client.repo,
            "base_url": client.base_url,
        },
        "pr": {
            "number": pr.get("number") or pr_number,
            "title": pr.get("title"),
            "author": user.get("login") or user.get("name"),
            "state": pr.get("state"),
            "branch": f"{head.get('ref')} -> {base.get('ref')}",
            "head_sha": head_sha,
            "head_ref": head.get("ref"),
            "base_ref": base.get("ref"),
            "stats": {
                "files_changed": len(changed_files),
                "commits": len(commits),
                "comments": len(comments),
                "inline_comments": inline_comments,
                "unresolved_comments": unresolved_comments,
                "additions": additions,
                "deletions": deletions,
            },
            "changed_files": changed_files,
        },
        "commits": commits,
        "comments": comments,
    }


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Fetch AtomGit PR context or submit maintainer-approved PR comments.")
    parser.add_argument("--pr", type=int, help="PR number. Can be inferred from --url.")
    parser.add_argument("--url", help="AtomGit PR URL, used to infer owner/repo/PR number.")
    parser.add_argument("--owner", default=DEFAULT_OWNER, help=f"Repository owner. Default: {DEFAULT_OWNER}")
    parser.add_argument("--repo", default=DEFAULT_REPO, help=f"Repository name. Default: {DEFAULT_REPO}")
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL, help=f"AtomGit API base URL. Default: {DEFAULT_BASE_URL}")
    parser.add_argument("--token-env", default="ATOMGIT_TOKEN", help="Environment variable containing AtomGit token.")
    parser.add_argument("--output-dir", default="./tmp", help="Output directory. Default: ./tmp")
    parser.add_argument("--no-comments", action="store_true", help="Skip fetching PR comments.")
    parser.add_argument("--comment-file", help="Submit a PR-level comment from this file.")
    parser.add_argument("--dry-run", action="store_true", help="Print the comment body without submitting it.")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    parsed_url = parse_pr_url(args.url)

    owner = parsed_url.get("owner") or args.owner
    repo = parsed_url.get("repo") or args.repo
    pr_number = parsed_url.get("pr") or args.pr
    if pr_number is None:
        print("error: PR number is required via --pr or --url", file=sys.stderr)
        return 2

    token = os.environ.get(args.token_env)
    if not token:
        print(f"error: environment variable {args.token_env} is not set", file=sys.stderr)
        return 2

    client = AtomGitClient(token=token, owner=owner, repo=repo, base_url=args.base_url)

    if args.comment_file:
        comment_path = Path(args.comment_file)
        body = comment_path.read_text(encoding="utf-8")
        if args.dry_run:
            print(body)
            return 0

        result = client.submit_pr_comment(int(pr_number), body)
        print(f"submitted comment to {owner}/{repo} PR #{pr_number}")
        if result.get("id"):
            print(f"comment_id: {result['id']}")
        return 0

    context = build_pr_context(client, int(pr_number), include_comments=not args.no_comments)

    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)
    repo_slug = repo.lower().replace("-", "_")
    output_file = output_dir / f"{repo_slug}_pr_{pr_number}_info.json"
    output_file.write_text(json.dumps(context, ensure_ascii=False, indent=2), encoding="utf-8")

    stats = context["pr"]["stats"]
    print(f"saved: {output_file}")
    print(f"repo: {owner}/{repo}")
    print(f"pr: #{pr_number} {context['pr'].get('title')}")
    print(
        "stats: "
        f"files={stats['files_changed']} commits={stats['commits']} "
        f"comments={stats['comments']} additions={stats['additions']} deletions={stats['deletions']}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
