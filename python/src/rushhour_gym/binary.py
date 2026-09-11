# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the Apache License, Version 2.0.
# SPDX-License-Identifier: Apache-2.0

"""Finding the ``rushhour-env`` binary that serves the game.

The Go side is the single source of truth for the rules, so the Python package
is useless without it. It is looked for in the places it plausibly is, built
from source when the checkout and a Go toolchain are at hand, and otherwise
fetched once from the GitHub release this package version belongs to.
"""

from __future__ import annotations

import hashlib
import os
import platform
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

__all__ = ["find_binary", "BinaryNotFound"]

_EXE = ".exe" if sys.platform == "win32" else ""
_BINARY_NAME = "rushhour-env" + _EXE
_GO_PACKAGE = "./cmd/rushhour-env"
_MODULE_LINE = "module rush-hour"

# Where a release lives, and what it is called on each platform the release
# workflow (.github/workflows/release.yml) builds for. The package version is
# the tag it fetches from, so a wheel and its engine cannot drift apart.
_RELEASES = "https://github.com/chrplr/Rush-Hour/releases/download"
_ASSETS: dict[tuple[str, str], str] = {
    ("linux", "x86_64"): "Rush-Hour-linux-x86_64.tar.gz",
    ("linux", "amd64"): "Rush-Hour-linux-x86_64.tar.gz",
    ("darwin", "arm64"): "Rush-Hour-macos-arm64.tar.gz",
    ("win32", "amd64"): "Rush-Hour-windows-x86_64.zip",
    ("win32", "x86_64"): "Rush-Hour-windows-x86_64.zip",
}
# Set to a non-empty value to never touch the network (an offline machine, or
# a test that must fail loudly rather than fetch something).
_OFFLINE_VAR = "RUSHHOUR_ENV_OFFLINE"


class BinaryNotFound(RuntimeError):
    """The environment server could not be located or built."""


def find_binary(explicit: str | os.PathLike[str] | None = None) -> str:
    """Return the path to the environment server.

    Tried in order: an explicit path, ``$RUSHHOUR_ENV_BIN``, the ``PATH``, the
    repository checkout this package lives in (a binary at its root, else a
    cached build from its sources when Go is installed), and finally the
    prebuilt one from the GitHub release matching this package's version,
    fetched once into the user cache. ``$RUSHHOUR_ENV_OFFLINE`` forbids that
    last step.
    """
    if explicit is not None:
        path = Path(explicit)
        if not path.exists():
            raise BinaryNotFound(f"no such binary: {path}")
        return str(path)

    env = os.environ.get("RUSHHOUR_ENV_BIN")
    if env:
        if not Path(env).exists():
            raise BinaryNotFound(f"$RUSHHOUR_ENV_BIN points at a missing file: {env}")
        return env

    on_path = shutil.which("rushhour-env")
    if on_path:
        return on_path

    repo = _find_repo()
    if repo is not None:
        built = repo / _BINARY_NAME
        if built.exists():
            return str(built)
        if shutil.which("go") is not None:
            return _build(repo)
        # A checkout without Go: the release binary serves as well as one
        # built here would, as long as the protocol has not moved.

    return _download()


def _find_repo() -> Path | None:
    """Walk up from this file looking for the Rush-Hour module root."""
    for parent in Path(__file__).resolve().parents:
        go_mod = parent / "go.mod"
        if go_mod.is_file() and _MODULE_LINE in go_mod.read_text(encoding="utf-8"):
            return parent
    return None


def _cache_dir() -> Path:
    base = os.environ.get("XDG_CACHE_HOME") or (Path.home() / ".cache")
    return Path(base) / "rushhour-gym"


def _build(repo: Path) -> str:
    """Build the server into the user cache, reusing it while it is current.

    Rebuilding is keyed on the Go sources' modification times: during
    development the binary must not silently lag behind an edited protocol,
    which is the one failure mode that produces confusing errors much later.
    """
    out = _cache_dir() / _BINARY_NAME
    newest = max(
        (p.stat().st_mtime for p in repo.rglob("*.go") if ".git" not in p.parts),
        default=0.0,
    )
    if out.exists() and out.stat().st_mtime >= newest:
        return str(out)

    out.parent.mkdir(parents=True, exist_ok=True)
    result = subprocess.run(
        ["go", "build", "-o", str(out), _GO_PACKAGE],
        cwd=repo,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise BinaryNotFound(
            f"go build {_GO_PACKAGE} failed in {repo}:\n{result.stderr.strip()}"
        )
    return str(out)


# ── Release download ─────────────────────────────────────────────────────────


def _release_tag() -> str:
    from . import __version__

    return "v" + __version__


def _asset_name() -> str | None:
    """The release archive for this machine, or None when none is built."""
    return _ASSETS.get((sys.platform, platform.machine().lower()))


def _download(tag: str | None = None, cache: Path | None = None) -> str:
    """Fetch this version's ``rushhour-env`` from its GitHub release, once.

    The archive is checked against the release's SHA256SUMS before anything
    is extracted, and lands in the user cache under the tag, so two package
    versions never share a binary. Subsequent calls find it there without
    touching the network.
    """
    tag = tag or _release_tag()
    cache = cache or _cache_dir()
    dest = cache / tag / _BINARY_NAME
    if dest.exists():
        return str(dest)

    manual = (
        f"Alternatively, download rushhour-env from "
        f"https://github.com/chrplr/Rush-Hour/releases/tag/{tag} and point "
        "$RUSHHOUR_ENV_BIN at it, or build it from the checkout with\n"
        f"    go build -o {_BINARY_NAME} {_GO_PACKAGE}"
    )
    if os.environ.get(_OFFLINE_VAR):
        raise BinaryNotFound(
            f"cannot find the rushhour-env binary, and ${_OFFLINE_VAR} is set, so it "
            f"was not fetched from the {tag} release.\n{manual}"
        )
    asset = _asset_name()
    if asset is None:
        raise BinaryNotFound(
            f"cannot find the rushhour-env binary, and the {tag} release has no "
            f"prebuilt one for {sys.platform}/{platform.machine()}.\n{manual}"
        )

    base = f"{_RELEASES}/{tag}"
    print(f"rushhour-gym: fetching {asset} from the {tag} release ...", file=sys.stderr)
    try:
        sums = _fetch(f"{base}/SHA256SUMS").decode("utf-8", "replace")
        archive = _fetch(f"{base}/{asset}")
    except urllib.error.URLError as e:
        raise BinaryNotFound(
            f"could not fetch {asset} from {base}: {e}\n{manual}"
        ) from e

    want = _expected_sum(sums, asset)
    got = hashlib.sha256(archive).hexdigest()
    if want is None or got != want:
        raise BinaryNotFound(
            f"{asset} from {base} does not match the release's SHA256SUMS "
            f"(got {got}, want {want}); not installing it.\n{manual}"
        )

    member = "Rush-Hour/" + _BINARY_NAME
    dest.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=dest.parent) as tmp:
        path = Path(tmp) / asset
        path.write_bytes(archive)
        out = Path(tmp) / _BINARY_NAME
        out.write_bytes(_extract(path, member))
        out.chmod(0o755)
        # A rename within the directory is atomic: a second process racing
        # for the same binary sees either nothing or the whole file.
        os.replace(out, dest)
    return str(dest)


def _fetch(url: str) -> bytes:
    with urllib.request.urlopen(url, timeout=60) as resp:
        return resp.read()


def _expected_sum(sums: str, asset: str) -> str | None:
    """The digest SHA256SUMS lists for asset (``<hex>  <name>`` per line)."""
    for line in sums.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1] == asset:
            return parts[0].lower()
    return None


def _extract(archive: Path, member: str) -> bytes:
    """The one file wanted from a release archive."""
    if archive.name.endswith(".zip"):
        with zipfile.ZipFile(archive) as zf:
            return zf.read(member)
    with tarfile.open(archive, "r:gz") as tf:
        f = tf.extractfile(member)
        if f is None:
            raise BinaryNotFound(f"{archive.name} has no {member}")
        return f.read()
