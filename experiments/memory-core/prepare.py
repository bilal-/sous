#!/usr/bin/env python3
"""Fetch a verified upstream revision and install its locked, isolated runtime."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import tarfile
from urllib.request import urlopen

from experiment import UPSTREAM_COMMIT

ARCHIVE_SHA256 = "e9aa5d47e2491aff23290bd76820d10af8a1063662d3171af3bbaeacabde16e9"


def verify_archive(path):
    if hashlib.sha256(path.read_bytes()).hexdigest() != ARCHIVE_SHA256:
        raise ValueError("upstream archive checksum differs; inspect before updating the pin")


def unpack(archive, destination):
    with tarfile.open(archive) as bundle:
        for member in bundle:
            path = PurePosixPath(member.name)
            if path.is_absolute() or ".." in path.parts:
                raise ValueError("unsafe archive path")
            # Never materialize links, devices, permissions, or ownership.
            if not (member.isfile() or member.isdir()):
                continue
            target = destination.joinpath(*path.parts)
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with bundle.extractfile(member) as source, target.open("wb") as output:
                    shutil.copyfileobj(source, output)


def prepare(destination, archive=None):
    destination.mkdir(parents=True, exist_ok=False)
    if archive is None:
        archive = destination / "upstream.tar.gz"
        url = "https://codeload.github.com/TencentCloud/TencentDB-Agent-Memory/tar.gz/" + UPSTREAM_COMMIT
        with urlopen(url, timeout=60) as source, archive.open("wb") as output:
            shutil.copyfileobj(source, output)
    verify_archive(archive)
    unpack(archive, destination)
    source = destination / ("TencentDB-Agent-Memory-" + UPSTREAM_COMMIT)
    core = source / "MemoryCore"
    package_path = core / "package.json"
    package = json.loads(package_path.read_text())
    # The upstream Dockerfile also removes agent peer dependencies and ignores
    # lifecycle scripts. Omit optional cloud integrations at the package root;
    # transitive native platform dependencies (tsx, jieba) remain available.
    for field in ("peerDependencies", "peerDependenciesMeta", "optionalDependencies"):
        package.pop(field, None)
    package_path.write_text(json.dumps(package, indent=2) + "\n")
    shutil.copyfile(Path(__file__).with_name("runtime-package-lock.json"), core / "package-lock.json")
    home = destination / "install-home"
    home.mkdir()
    env = {"PATH": os.environ["PATH"], "HOME": str(home), "TMPDIR": str(destination),
           "npm_config_cache": str(destination / "npm-cache")}
    subprocess.run(["npm", "ci", "--omit=dev", "--ignore-scripts", "--legacy-peer-deps",
                    "--no-audit", "--no-fund"], cwd=core, env=env, check=True)
    return source


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--destination", type=Path, required=True, help="new disposable directory")
    parser.add_argument("--archive", type=Path, help="reuse an already downloaded archive; checksum still checked")
    args = parser.parse_args()
    source = prepare(args.destination.resolve(), args.archive.resolve() if args.archive else None)
    print("Prepared upstream source: " + str(source))


if __name__ == "__main__":
    main()
