#!/usr/bin/env python3
"""Prepare and validate an openEuler Embedded MCS QEMU package.

This helper intentionally stops at package/SDK validation. Starting QEMU,
logging in, and deploying artifacts still depend on the user's environment.
"""

from __future__ import annotations

import argparse
import html.parser
import os
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
import urllib.request
from pathlib import Path


DEFAULT_INDEX_URL = "https://build-logs.openeuler.openatom.cn:38080/packages/master/aarch64/qemu-aarch64-hmi-mcs-ros/"
MIN_MCS_ARCHIVE_SIZE = 900 * 1024 * 1024
SDK_INTERP_SUFFIX = "/sysroots/x86_64-openeulersdk-linux/lib/ld-linux-x86-64.so.2"
SDK_INTERP_LIMIT = 98
INDEX_TIMEOUT = 30
DOWNLOAD_TIMEOUT = 60


def default_cache_dir() -> Path:
    return Path(os.environ.get("MICA_CACHE_HOME", "~/.mica")).expanduser()


class LinkParser(html.parser.HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.links: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag != "a":
            return
        for key, value in attrs:
            if key == "href" and value:
                self.links.append(value)


def run(cmd: list[str], *, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(cmd, check=True, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env)


def latest_archive_url(index_url: str) -> str:
    with urllib.request.urlopen(index_url, timeout=INDEX_TIMEOUT) as response:
        content = response.read().decode("utf-8", errors="replace")
    parser = LinkParser()
    parser.feed(content)
    names = sorted({link for link in parser.links if re.fullmatch(r"\d+\.tar\.gz", link)})
    if not names:
        raise RuntimeError(f"no timestamped tarball found in {index_url}")
    return urllib.request.urljoin(index_url, names[-1])


def download(url: str, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    with urllib.request.urlopen(url, timeout=DOWNLOAD_TIMEOUT) as response, dest.open("wb") as out:
        shutil.copyfileobj(response, out)


def validate_archive(path: Path) -> None:
    size = path.stat().st_size
    if size < MIN_MCS_ARCHIVE_SIZE:
        raise RuntimeError(
            f"archive is too small for qemu-aarch64-hmi-mcs-ros: {size} bytes; "
            "it may be the plain qemu-aarch64 package with the same filename"
        )
    if not tarfile.is_tarfile(path):
        raise RuntimeError(f"not a valid tar archive: {path}")


def validate_package_dir(package_dir: Path) -> None:
    required = [
        "zImage",
        "vmlinux",
        "openeuler-glibc-x86_64-openeuler-image-aarch64-qemu-aarch64-toolchain-latest.sh",
    ]
    for name in required:
        if not (package_dir / name).exists():
            raise RuntimeError(f"missing {name} in {package_dir}")
    if not list(package_dir.glob("openeuler-image-qemu-aarch64-*.rootfs.cpio.gz")):
        raise RuntimeError(f"missing rootfs cpio.gz in {package_dir}")


def extract_archive(path: Path, dest: Path) -> Path:
    dest.mkdir(parents=True, exist_ok=True)
    with tarfile.open(path) as tar:
        top_dirs = {member.name.split("/", 1)[0] for member in tar.getmembers() if member.name}
        if len(top_dirs) != 1:
            raise RuntimeError(f"expected one top-level directory, got: {sorted(top_dirs)}")
        package_dir = dest / next(iter(top_dirs))
        if package_dir.exists():
            validate_package_dir(package_dir)
            return package_dir
        try:
            tar.extractall(dest, filter="data")
        except TypeError:
            tar.extractall(dest)
    validate_package_dir(package_dir)
    return package_dir


def install_sdk(package_dir: Path, sdk_dir: Path) -> None:
    installer = package_dir / "openeuler-glibc-x86_64-openeuler-image-aarch64-qemu-aarch64-toolchain-latest.sh"
    validate_sdk_install_prefix(sdk_dir)
    run(["sh", str(installer), "-y", "-d", str(sdk_dir)])


def validate_sdk_install_prefix(sdk_dir: Path) -> None:
    sdk_dir = sdk_dir.expanduser().resolve()
    interp_len = len(str(sdk_dir) + SDK_INTERP_SUFFIX) + 1
    if interp_len > SDK_INTERP_LIMIT:
        raise RuntimeError(
            "SDK install path is too long for Yocto SDK native ELF relocation: "
            f"{interp_len} bytes needed, limit is {SDK_INTERP_LIMIT}. "
            "Use a shorter --sdk-dir or MICA_CACHE_HOME. The default SDK layout is <cache-root>/<timestamp>."
        )


def sdk_env(sdk_dir: Path) -> dict[str, str]:
    sdk_dir = sdk_dir.expanduser()
    setup = sdk_dir / "environment-setup-aarch64-openeuler-linux"
    if not setup.exists():
        raise RuntimeError(f"SDK setup script not found: {setup}")
    command = f". {shlex.quote(str(setup))} >/dev/null && env"
    proc = subprocess.run(["bash", "-lc", command], check=True, text=True, stdout=subprocess.PIPE)
    env = {}
    for line in proc.stdout.splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            env[key] = value
    return env


def validate_sdk(sdk_dir: Path) -> None:
    sdk_dir = sdk_dir.expanduser()
    env = sdk_env(sdk_dir)
    for key in ["SDKTARGETSYSROOT", "KERNEL_SRC"]:
        if not env.get(key):
            raise RuntimeError(f"{key} is not set after sourcing SDK")
    sysroot = Path(env["SDKTARGETSYSROOT"])
    checks = [
        sysroot / "usr/include/metal/alloc.h",
        sysroot / "usr/lib64/libmetal.so",
        sysroot / "usr/lib64/libopen_amp.so",
        sysroot / "lib64/libsysfs.so",
    ]
    missing = [str(path) for path in checks if not path.exists()]
    if missing:
        raise RuntimeError("SDK lacks required MICA dependencies:\n" + "\n".join(missing))
    setup = shlex.quote(str(sdk_dir / "environment-setup-aarch64-openeuler-linux"))
    run(["bash", "-lc", f". {setup} >/dev/null && aarch64-openeuler-linux-gcc -v >/dev/null"])


def main() -> int:
    parser = argparse.ArgumentParser(description="Prepare and validate MCS QEMU package and SDK")
    parser.add_argument("--index-url", default=DEFAULT_INDEX_URL)
    parser.add_argument("--archive", type=Path, help="existing qemu-aarch64-hmi-mcs-ros tarball")
    parser.add_argument("--cache-dir", type=Path, default=default_cache_dir())
    parser.add_argument("--download-dir", type=Path)
    parser.add_argument("--work-dir", type=Path)
    parser.add_argument("--sdk-dir", type=Path)
    parser.add_argument("--download", action="store_true", help="download latest tarball when --archive is absent")
    parser.add_argument("--install-sdk", action="store_true")
    parser.add_argument("--force-update", action="store_true", help="redownload archive and reinstall SDK even when cache exists")
    args = parser.parse_args()

    args.cache_dir = args.cache_dir.expanduser()
    archive_dir = (args.download_dir or (args.cache_dir / "archives")).expanduser()
    package_root = (args.work_dir or (args.cache_dir / "packages")).expanduser()
    archive = args.archive
    source_url = None
    if archive is None:
        source_url = latest_archive_url(args.index_url)
        archive = archive_dir / Path(source_url).name
        if args.download and (args.force_update or not archive.exists()):
            download(source_url, archive)
        elif not archive.exists():
            raise RuntimeError(f"archive not found: {archive}; rerun with --download")

    archive = archive.expanduser()
    validate_archive(archive)
    package_dir = extract_archive(archive, package_root)
    sdk_dir = (args.sdk_dir or (args.cache_dir / package_dir.name)).expanduser()
    sdk_setup = sdk_dir / "environment-setup-aarch64-openeuler-linux"
    if args.install_sdk and (args.force_update or not sdk_setup.exists()):
        install_sdk(package_dir, sdk_dir)
    if sdk_dir.exists():
        validate_sdk(sdk_dir)

    rootfs = next(package_dir.glob("openeuler-image-qemu-aarch64-*.rootfs.cpio.gz"))
    print("MCS QEMU package validated")
    if source_url:
        print(f"source_url={source_url}")
    print(f"archive={archive}")
    print(f"package_dir={package_dir}")
    print(f"sdk_dir={sdk_dir}")
    print("baremetal_qemu_append=root=/dev/ram0 rw maxcpus=3 console=ttyAMA0 ramdisk_size=524288")
    print(f"kernel={package_dir / 'zImage'}")
    print(f"rootfs={rootfs}")
    print("dtb=qemu.dtb generated by tools/create_dtb.sh qemu-a53")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1)
