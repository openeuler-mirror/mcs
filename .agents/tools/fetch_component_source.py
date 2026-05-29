#!/usr/bin/env python3
"""Fetch external component source trees for MICA analysis workflows.

Two source acquisition modes are supported:
1. Direct git clone for standalone component repositories.
2. Manifest-driven export for components managed in yocto-meta-openeuler.

The script creates a local components/ directory under the current workspace by
default. Large repositories may take a long time to download.
"""

from __future__ import annotations

import argparse
import io
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request
from pathlib import Path

try:
    import yaml
except ImportError as exc:  # pragma: no cover - environment guard
    raise SystemExit("PyYAML is required to use this tool") from exc


DIRECT_COMPONENTS: dict[str, dict[str, str]] = {
    "yocto-meta-openeuler": {
        "url": "https://atomgit.com/openeuler/yocto-meta-openeuler.git",
        "branch": "master",
    },
    "uniproton": {
        "url": "https://atomgit.com/openeuler/UniProton.git",
        "branch": "master",
        "dest_name": "UniProton",
    },
}

MANIFEST_COMPONENTS = {
    "jailhouse": "Jailhouse",
    "openamp": "OpenAMP",
    "libmetal": "libmetal",
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Fetch external component sources into a local components directory.")
    parser.add_argument("component", help="Component name, such as yocto-meta-openeuler, uniproton, jailhouse, openamp, or libmetal.")
    parser.add_argument("--url", help="Explicit repository URL for direct clone mode.")
    parser.add_argument("--branch", help="Optional branch or tag for direct clone mode.")
    parser.add_argument("--dest-root", default="components", help="Destination root directory. Default: ./components")
    parser.add_argument("--dest-name", help="Destination directory name. Default: derived from component name")
    parser.add_argument(
        "--yocto-root",
        default="components/yocto-meta-openeuler",
        help="Path to local yocto-meta-openeuler checkout used for manifest-driven components. Default: ./components/yocto-meta-openeuler",
    )
    return parser.parse_args()


def run(command: list[str], workdir: Path | None = None) -> None:
    subprocess.run(command, cwd=workdir, check=True)


def clone_repo(url: str, dest_dir: Path, branch: str | None) -> None:
    command = ["git", "clone"]
    if branch:
        command.extend(["--branch", branch])
    command.extend([url, str(dest_dir)])
    run(command)


def git_export_component(dest_dir: Path, remote_url: str, version: str) -> None:
    with tempfile.TemporaryDirectory(prefix="fetch-component-") as temp_dir_str:
        temp_dir = Path(temp_dir_str) / "repo"
        clone_repo(remote_url, temp_dir, None)
        run(["git", "checkout", version], workdir=temp_dir)
        shutil.move(str(temp_dir), str(dest_dir))


def find_primary_tarball(repo_dir: Path) -> Path:
    candidates = sorted(repo_dir.glob("*.tar.gz")) + sorted(repo_dir.glob("*.tgz")) + sorted(repo_dir.glob("*.tar.xz"))
    if not candidates:
        raise SystemExit(f"error: no source tarball found under {repo_dir}")
    return candidates[0]


def extract_repo_tarball(repo_dir: Path, source_dir: Path) -> Path:
    tarball = find_primary_tarball(repo_dir)
    temp_root = repo_dir / ".extract_tmp"
    if temp_root.exists():
        shutil.rmtree(temp_root)
    temp_root.mkdir(parents=True, exist_ok=True)

    with tarfile.open(tarball, mode="r:*") as archive:
        archive.extractall(path=temp_root, filter="data")

    children = [child for child in temp_root.iterdir()]
    if len(children) == 1 and children[0].is_dir():
        extracted_root = children[0]
    else:
        extracted_root = temp_root / "source-root"
        extracted_root.mkdir(parents=True, exist_ok=True)
        for child in list(children):
            shutil.move(str(child), extracted_root / child.name)

    if source_dir.exists():
        shutil.rmtree(source_dir)
    shutil.move(str(extracted_root), str(source_dir))
    shutil.rmtree(temp_root, ignore_errors=True)
    return tarball


def ensure_yocto_manifest(yocto_root: Path) -> Path:
    manifest = yocto_root / ".oebuild" / "manifest.yaml"
    if manifest.exists():
        return manifest

    info = DIRECT_COMPONENTS["yocto-meta-openeuler"]
    yocto_root.parent.mkdir(parents=True, exist_ok=True)
    if not yocto_root.exists():
        clone_repo(info["url"], yocto_root, info.get("branch"))
    return manifest


def load_manifest_entry(manifest_path: Path, component_key: str) -> dict[str, str]:
    data = yaml.safe_load(manifest_path.read_text(encoding="utf-8")) or {}
    manifest_list = data.get("manifest_list") or {}
    entry_name = MANIFEST_COMPONENTS[component_key]
    entry = manifest_list.get(entry_name)
    if not entry:
        raise SystemExit(f"error: component '{entry_name}' not found in {manifest_path}")
    return {"name": entry_name, "url": entry["remote_url"], "version": entry["version"]}


def tarball_url(remote_url: str, version: str) -> str:
    repo_url = remote_url.removesuffix(".git")
    return f"{repo_url}/archive/{version}.tar.gz"


def export_manifest_component(dest_dir: Path, remote_url: str, version: str) -> None:
    url = tarball_url(remote_url, version)
    try:
        with urllib.request.urlopen(url, timeout=120) as response:
            payload = response.read()

        with tarfile.open(fileobj=io.BytesIO(payload), mode="r:gz") as archive:
            members = archive.getmembers()
            root_names = {member.name.split("/", 1)[0] for member in members if member.name}
            temp_root = dest_dir.parent / f".{dest_dir.name}.tmp"
            if temp_root.exists():
                shutil.rmtree(temp_root)
            temp_root.mkdir(parents=True, exist_ok=True)
            archive.extractall(path=temp_root)

        if len(root_names) != 1:
            raise RuntimeError(f"unexpected archive layout from {url}")

        extracted_root = temp_root / next(iter(root_names))
        extracted_root.rename(dest_dir)
        temp_root.rmdir()
    except Exception:
        git_export_component(dest_dir, remote_url, version)


def materialize_manifest_component(dest_dir: Path, remote_url: str, version: str) -> tuple[Path, Path]:
    export_manifest_component(dest_dir, remote_url, version)
    source_dir = dest_dir / "source"
    tarball = extract_repo_tarball(dest_dir, source_dir)
    return tarball, source_dir


def main() -> int:
    args = parse_args()
    component = args.component.strip().lower()
    dest_root = Path(args.dest_root)
    dest_root.mkdir(parents=True, exist_ok=True)

    if component in DIRECT_COMPONENTS:
        info = DIRECT_COMPONENTS[component]
        dest_name = args.dest_name or info.get("dest_name") or args.component
        dest_dir = dest_root / dest_name
        if dest_dir.exists():
            print(f"exists: {dest_dir}")
            return 0

        url = args.url or info["url"]
        branch = args.branch or info.get("branch")
        clone_repo(url, dest_dir, branch)
        print(f"cloned: {component}")
        print(f"url: {url}")
        print(f"path: {dest_dir}")
        if branch:
            print(f"branch: {branch}")
        return 0

    if component in MANIFEST_COMPONENTS:
        dest_name = args.dest_name or MANIFEST_COMPONENTS[component]
        dest_dir = dest_root / dest_name
        if dest_dir.exists():
            source_dir = dest_dir / "source"
            if source_dir.exists():
                print(f"exists: {dest_dir}")
                print(f"source: {source_dir}")
                return 0

            tarball = extract_repo_tarball(dest_dir, source_dir)
            print(f"expanded: {dest_dir}")
            print(f"tarball: {tarball}")
            print(f"source: {source_dir}")
            return 0

        manifest_path = ensure_yocto_manifest(Path(args.yocto_root))
        entry = load_manifest_entry(manifest_path, component)
        tarball, source_dir = materialize_manifest_component(dest_dir, entry["url"], entry["version"])
        print(f"exported: {entry['name']}")
        print(f"url: {entry['url']}")
        print(f"version: {entry['version']}")
        print(f"path: {dest_dir}")
        print(f"tarball: {tarball}")
        print(f"source: {source_dir}")
        return 0

    if args.url:
        dest_name = args.dest_name or args.component
        dest_dir = dest_root / dest_name
        if dest_dir.exists():
            print(f"exists: {dest_dir}")
            return 0
        clone_repo(args.url, dest_dir, args.branch)
        print(f"cloned: {component}")
        print(f"url: {args.url}")
        print(f"path: {dest_dir}")
        if args.branch:
            print(f"branch: {args.branch}")
        return 0

    print(f"error: unknown component '{args.component}', please provide --url", file=sys.stderr)
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
