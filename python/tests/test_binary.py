# Copyright (2026) Christophe Pallier <christophe@pallier.org>
# Distributed under the Apache License, Version 2.0.
# SPDX-License-Identifier: Apache-2.0
"""The release download in binary.py, without the network: urlopen is replaced
by a fake release serving one archive and its SHA256SUMS."""

from __future__ import annotations

import hashlib
import io
import os
import stat
import sys
import tarfile
import zipfile
from pathlib import Path

import pytest

from rushhour_gym import binary
from rushhour_gym.binary import BinaryNotFound

TAG = "v9.9.9"
PAYLOAD = b"#!/bin/sh\necho fake rushhour-env\n"


def _archive(name: str, member: str, payload: bytes) -> bytes:
    buf = io.BytesIO()
    if name.endswith(".zip"):
        with zipfile.ZipFile(buf, "w") as zf:
            zf.writestr(member, payload)
    else:
        with tarfile.open(fileobj=buf, mode="w:gz") as tf:
            info = tarfile.TarInfo(member)
            info.size = len(payload)
            tf.addfile(info, io.BytesIO(payload))
    return buf.getvalue()


class FakeRelease:
    """Serves {tag}/{asset} and {tag}/SHA256SUMS, counting requests."""

    def __init__(self, asset: str, archive: bytes, sums: str | None = None):
        self.asset, self.archive = asset, archive
        digest = hashlib.sha256(archive).hexdigest()
        self.sums = sums if sums is not None else f"{digest}  {asset}\n"
        self.requests: list[str] = []

    def urlopen(self, url: str, timeout: float = 0):
        self.requests.append(url)
        assert url.startswith(f"{binary._RELEASES}/{TAG}/"), url
        name = url.rsplit("/", 1)[1]
        if name == "SHA256SUMS":
            body = self.sums.encode()
        elif name == self.asset:
            body = self.archive
        else:
            raise binary.urllib.error.URLError(f"404 {name}")
        return io.BytesIO(body)


@pytest.fixture
def release(monkeypatch):
    asset = binary._asset_name()
    if asset is None:
        pytest.skip("no release asset for this platform")
    member = "Rush-Hour/" + binary._BINARY_NAME
    rel = FakeRelease(asset, _archive(asset, member, PAYLOAD))
    monkeypatch.setattr(binary.urllib.request, "urlopen", rel.urlopen)
    monkeypatch.delenv(binary._OFFLINE_VAR, raising=False)
    return rel


def test_download_fetches_verifies_and_caches(release, tmp_path):
    path = Path(binary._download(TAG, tmp_path))
    assert path == tmp_path / TAG / binary._BINARY_NAME
    assert path.read_bytes() == PAYLOAD
    if sys.platform != "win32":
        assert path.stat().st_mode & stat.S_IXUSR
    # Checksums first, then the archive; and nothing more once it is cached.
    assert [u.rsplit("/", 1)[1] for u in release.requests] == ["SHA256SUMS", release.asset]
    assert binary._download(TAG, tmp_path) == str(path)
    assert len(release.requests) == 2


def test_checksum_mismatch_installs_nothing(release, tmp_path):
    release.sums = f"{'0' * 64}  {release.asset}\n"
    with pytest.raises(BinaryNotFound, match="SHA256SUMS"):
        binary._download(TAG, tmp_path)
    assert not (tmp_path / TAG / binary._BINARY_NAME).exists()


def test_missing_checksum_line_installs_nothing(release, tmp_path):
    release.sums = "deadbeef  something-else.tar.gz\n"
    with pytest.raises(BinaryNotFound, match="SHA256SUMS"):
        binary._download(TAG, tmp_path)


def test_offline_never_fetches(release, tmp_path, monkeypatch):
    monkeypatch.setenv(binary._OFFLINE_VAR, "1")
    with pytest.raises(BinaryNotFound, match=binary._OFFLINE_VAR):
        binary._download(TAG, tmp_path)
    assert release.requests == []


def test_network_failure_names_the_url(release, tmp_path, monkeypatch):
    def down(url, timeout=0):
        raise binary.urllib.error.URLError("no route to host")

    monkeypatch.setattr(binary.urllib.request, "urlopen", down)
    with pytest.raises(BinaryNotFound, match="no route to host"):
        binary._download(TAG, tmp_path)


def test_release_tag_is_the_package_version():
    import rushhour_gym

    assert binary._release_tag() == "v" + rushhour_gym.__version__


def test_find_binary_falls_through_to_download(release, tmp_path, monkeypatch):
    """With nothing local -- no env var, nothing on PATH, no checkout -- the
    lookup ends in the release download rather than an error."""
    monkeypatch.delenv("RUSHHOUR_ENV_BIN", raising=False)
    monkeypatch.setattr(binary.shutil, "which", lambda name: None)
    monkeypatch.setattr(binary, "_find_repo", lambda: None)
    monkeypatch.setattr(binary, "_cache_dir", lambda: tmp_path)
    monkeypatch.setattr(binary, "_release_tag", lambda: TAG)
    assert Path(binary.find_binary()).read_bytes() == PAYLOAD


def test_extract_reads_the_member_from_both_formats(tmp_path):
    member = "Rush-Hour/" + binary._BINARY_NAME
    for name in ("x.tar.gz", "x.zip"):
        p = tmp_path / name
        p.write_bytes(_archive(name, member, PAYLOAD))
        assert binary._extract(p, member) == PAYLOAD
