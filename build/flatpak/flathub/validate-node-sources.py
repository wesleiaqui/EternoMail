#!/usr/bin/env python3
"""Check lockfile coverage, then exercise the declared cache with offline npm ci.

Run on each native Linux builder (Node 24). Downloads happen only while filling
an empty temporary cache; npm ci itself remains offline. No checkout files or
user npm caches are changed. --check-only skips downloads and installation.
"""
import argparse
import base64
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parents[3]
SOURCES = ROOT / 'build/flatpak/flathub/node-sources.json'


def check_sources(lock, sources):
    files = {s['url']: s for s in sources if s['type'] == 'file'}
    indices = {}
    for source in sources:
        if source['type'] != 'inline':
            continue
        checksum, record = source['contents'].strip().split('\t', 1)
        if checksum != hashlib.sha1(record.encode()).hexdigest():
            raise ValueError('Invalid npm cache index checksum')
        entry = json.loads(record)
        url = entry['metadata']['url']
        key = 'make-fetch-happen:request-cache:' + url
        key_hash = hashlib.sha256(key.encode()).hexdigest()
        expected = f'flatpak-node/npm-cache/_cacache/index-v5/{key_hash[:2]}/{key_hash[2:4]}'
        if entry['key'] != key or source['dest'] != expected or source['dest-filename'] != key_hash[4:]:
            raise ValueError(f'Invalid npm cache index path: {url}')
        indices[url] = entry

    # Include nested, development, optional and platform-specific packages.
    # npm chooses the appropriate optional binaries on each native runner.
    for name, package in lock['packages'].items():
        if not name:
            continue
        url, integrity = package.get('resolved'), package.get('integrity')
        if not url or not integrity or url not in files or url not in indices:
            raise ValueError(f'Missing vendored dependency: {name}')
        algorithm, encoded = integrity.split('-', 1)
        digest = base64.b64decode(encoded, validate=True).hex()
        source = files[url]
        expected = f'flatpak-node/npm-cache/_cacache/content-v2/{algorithm}/{digest[:2]}/{digest[2:4]}'
        if (source.get(algorithm) != digest or source['dest'] != expected
                or source['dest-filename'] != digest[4:]
                or indices[url]['integrity'] != integrity or source.get('only-arches')):
            raise ValueError(f'Incorrect vendored dependency: {name}')
    return list(files.values())


def materialize_file(root, source):
    path = root / source['dest'] / source['dest-filename']
    path.parent.mkdir(parents=True, exist_ok=True)
    algorithm = next(a for a in ('sha512', 'sha256', 'sha1') if a in source)
    digest = hashlib.new(algorithm)
    with urlopen(source['url'], timeout=120) as response, path.open('wb') as output:
        while chunk := response.read(1024 * 1024):
            digest.update(chunk)
            output.write(chunk)
    if digest.hexdigest() != source[algorithm]:
        raise ValueError(f'Tarball checksum mismatch: {source["url"]}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check-only', action='store_true')
    args = parser.parse_args()
    sources = json.loads(SOURCES.read_text())
    lock = json.loads((ROOT / 'frontend/package-lock.json').read_text())
    files = check_sources(lock, sources)
    print(f'Validated {len(lock["packages"]) - 1} lockfile entries / {len(files)} tarballs', flush=True)
    if args.check_only:
        return

    with tempfile.TemporaryDirectory(prefix='flatpak-npm-check-') as directory:
        root = Path(directory)
        with ThreadPoolExecutor(max_workers=8) as pool:
            list(pool.map(lambda source: materialize_file(root, source), files))
        for source in sources:
            if source['type'] == 'inline':
                path = root / source['dest'] / source['dest-filename']
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(source['contents'])
        frontend = root / 'frontend'
        frontend.mkdir()
        for name in ('package.json', 'package-lock.json'):
            shutil.copyfile(ROOT / 'frontend' / name, frontend / name)
        # Keep lifecycle scripts enabled: esbuild's installation is part of the
        # real offline build. CLI flags override ambient npm cache/omit settings.
        env = dict(os.environ, npm_config_offline='true',
                   npm_config_cache=str(root / 'flatpak-node/npm-cache'))
        subprocess.run(['npm', 'ci', '--offline', '--legacy-peer-deps',
                        '--include=dev', '--include=optional', '--ignore-scripts=false',
                        '--no-audit', '--no-fund'], cwd=frontend, env=env, check=True)
        # A missing optional native package can otherwise be silently skipped by
        # npm, leaving the later Vite build broken on just one architecture.
        subprocess.run(['node', '-e',
                        "require('esbuild').transformSync('let x = 1'); require('rollup');"],
                       cwd=frontend, env=env, check=True)
    print('Offline npm install and native build dependencies passed', flush=True)


if __name__ == '__main__':
    main()
