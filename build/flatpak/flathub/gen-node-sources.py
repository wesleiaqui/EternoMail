#!/usr/bin/env python3
"""
Generate node-sources.json for Flatpak offline npm builds.

Reads frontend/package-lock.json and produces a Flatpak sources file that
populates the npm cache so `npm install --offline` works in the sandbox.

Usage:
    python3 build/flatpak/flathub/gen-node-sources.py

Output:
    build/flatpak/flathub/node-sources.json

This replaces flatpak-node-generator which stopped generating entries for
non-optional (pure JS) packages in version 0.1.1.
"""

import base64
import hashlib
import json
import os
import re
import sys

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.abspath(os.path.join(SCRIPT_DIR, '..', '..', '..'))
LOCKFILE = os.path.join(REPO_ROOT, 'frontend', 'package-lock.json')
OUTPUT = os.path.join(SCRIPT_DIR, 'node-sources.json')

# Map esbuild platform suffixes to Flatpak architecture names.
# Only Linux platforms are relevant for Flatpak builds.
ESBUILD_ARCH_MAP = {
    'linux-arm':     'arm',
    'linux-arm64':   'aarch64',
    'linux-ia32':    'i386',
    'linux-loong64': 'loongarch64',
    'linux-mips64el':'mips64el',
    'linux-ppc64':   'ppc64le',
    'linux-riscv64': 'riscv64',
    'linux-s390x':   's390x',
    'linux-x64':     'x86_64',
}


def parse_integrity(integrity: str) -> tuple[str, str]:
    """Parse an SSRI integrity string into (algorithm, hex_hash)."""
    algo, hash_b64 = integrity.split('-', 1)
    hash_bytes = base64.b64decode(hash_b64)
    return algo, hash_bytes.hex()


def make_cache_file_entry(url: str, algo: str, hash_hex: str) -> dict:
    """Create a file entry that downloads a tarball into the npm content cache."""
    prefix1 = hash_hex[:2]
    prefix2 = hash_hex[2:4]
    return {
        "type": "file",
        "url": url,
        algo: hash_hex,
        "dest-filename": hash_hex[4:],
        "dest": f"flatpak-node/npm-cache/_cacache/content-v2/{algo}/{prefix1}/{prefix2}",
    }


def make_cache_index_entry(url: str, integrity: str) -> dict:
    """Create an inline entry that writes an npm cache index record."""
    cache_key = f"make-fetch-happen:request-cache:{url}"
    key_hash = hashlib.sha256(cache_key.encode()).hexdigest()

    cache_meta = {
        "key": cache_key,
        "integrity": integrity,
        "time": 0,
        "size": 0,
        "metadata": {
            "url": url,
            "reqHeaders": {},
            "resHeaders": {},
        },
    }

    # cacache hashes the serialized record, not the cache key.
    record = json.dumps(cache_meta, separators=(',', ': '))
    content_hash = hashlib.sha1(record.encode()).hexdigest()
    return {
        "type": "inline",
        "contents": f"\n{content_hash}\t{record}\n",
        "dest-filename": key_hash[4:],
        "dest": f"flatpak-node/npm-cache/_cacache/index-v5/{key_hash[:2]}/{key_hash[2:4]}",
    }


def make_esbuild_archive_entry(url: str, hash_hex: str, pkg_name: str,
                                version: str, flatpak_arch: str) -> dict:
    """Create an archive entry that extracts an esbuild binary for a platform."""
    return {
        "type": "archive",
        "url": url,
        "strip-components": 1,
        "sha512": hash_hex,
        "dest": f"flatpak-node/cache/esbuild/.package/{pkg_name}@{version}",
        "only-arches": [flatpak_arch],
    }


def make_esbuild_shell_entry(pkg_name: str, version: str,
                              flatpak_arch: str) -> dict:
    """Create a shell entry that symlinks an esbuild binary into the bin dir."""
    return {
        "type": "shell",
        "commands": [
            'mkdir -p "bin/@esbuild"',
            f'cp ".package/{pkg_name}@{version}/bin/esbuild" "bin/@esbuild/{pkg_name.split("/")[-1]}@{version}"',
            f'ln -sf "{pkg_name.split("/")[-1]}@{version}" "bin/esbuild-current"',
        ],
        "dest": "flatpak-node/cache/esbuild",
        "only-arches": [flatpak_arch],
    }


def main():
    with open(LOCKFILE) as f:
        lock = json.load(f)

    pkgs = lock.get('packages', {})

    file_entries = []
    inline_entries = []
    esbuild_archive_entries = []
    esbuild_shell_entries = []

    # Track unique resolved URLs to avoid duplicates (nested node_modules
    # can reference the same tarball)
    seen_urls = set()

    for key in sorted(pkgs):
        if not key.startswith('node_modules/'):
            continue

        val = pkgs[key]
        resolved = val.get('resolved', '')
        integrity = val.get('integrity', '')

        if not resolved.startswith("https://") or not integrity:
            raise ValueError(f"Cannot vendor {key}: HTTPS resolved URL and integrity required")

        # Skip duplicate URLs (e.g. nested @esbuild packages)
        if resolved in seen_urls:
            continue
        seen_urls.add(resolved)

        algo, hash_hex = parse_integrity(integrity)

        # npm cache entries (file + index) for every package
        file_entries.append(make_cache_file_entry(resolved, algo, hash_hex))
        inline_entries.append(make_cache_index_entry(resolved, integrity))

        # esbuild platform binary entries (archive + shell symlink)
        esbuild_match = re.match(r'@esbuild/(linux-\w+)', key.split('node_modules/')[-1])
        if esbuild_match:
            platform = esbuild_match.group(1)
            flatpak_arch = ESBUILD_ARCH_MAP.get(platform)
            if flatpak_arch:
                version = val['version']
                pkg_name = f"@esbuild/{platform}"
                esbuild_archive_entries.append(
                    make_esbuild_archive_entry(resolved, hash_hex, pkg_name,
                                                version, flatpak_arch)
                )
                esbuild_shell_entries.append(
                    make_esbuild_shell_entry(pkg_name, version, flatpak_arch)
                )

    # Setup entries (node-gyp headers + npmrc + final shell)
    setup_entries = [
        {
            "type": "script",
            "commands": [
                'version=$(node --version | sed "s/^v//")',
                'nodedir=$(dirname "$(dirname "$(which node)")")',
                'mkdir -p "flatpak-node/cache/node-gyp/$version"',
                'ln -s "$nodedir/include" "flatpak-node/cache/node-gyp/$version/include"',
                'echo 11 > "flatpak-node/cache/node-gyp/$version/installVersion"',
            ],
            "dest-filename": "setup_sdk_node_headers.sh",
            "dest": "flatpak-node",
        },
        {
            "type": "shell",
            "commands": [
                "bash flatpak-node/setup_sdk_node_headers.sh",
            ],
        },
    ]

    # Assemble: cache entries, esbuild entries, then setup
    all_entries = (
        file_entries
        + inline_entries
        + esbuild_archive_entries
        + esbuild_shell_entries
        + setup_entries
    )

    with open(OUTPUT, 'w') as f:
        json.dump(all_entries, f, indent=4)
        f.write('\n')

    print(f"Lockfile: {LOCKFILE}")
    print(f"Output:   {OUTPUT}")
    print(f"  {len(file_entries)} file entries (npm cache tarballs)")
    print(f"  {len(inline_entries)} inline entries (npm cache indices)")
    print(f"  {len(esbuild_archive_entries)} esbuild archive entries")
    print(f"  {len(esbuild_shell_entries)} esbuild shell entries")
    print(f"  {len(setup_entries)} setup entries")
    print(f"  {len(all_entries)} total entries")


if __name__ == '__main__':
    main()
