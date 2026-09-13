#!/usr/bin/env python3
"""Refuse tracked or staged files that contain private values or credentials.

Private values come from the ignored `.virmill-local/private-values.json` (see
docs/repository-hygiene.md); credentials are checked everywhere. Reports name
the file, line and placeholder, never the value itself.
"""
import argparse
import os
from pathlib import Path
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import private_values

ROOT = private_values.ROOT
# Vendored upstream source is reproduced byte-for-byte and holds upstream test keys.
SKIP = ('vendor/',)


def git(*args, binary=False):
    return subprocess.run(['git', *args], cwd=ROOT, capture_output=True, check=True, text=not binary).stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument('--staged', action='store_true', help='check only staged changes (pre-commit)')
    a = parser.parse_args()
    listing = git('diff', '--cached', '--name-only', '--diff-filter=ACMR', '-z') if a.staged else git('ls-files', '-z')
    # A CI runner's home directory is not private; the private list still applies locally.
    rules = private_values.load(include_home=not os.environ.get('CI'))
    if not private_values.CONFIG.is_file():
        print('No .virmill-local/private-values.json; checking credentials only.', file=sys.stderr)
    found = 0
    for name in filter(None, listing.split('\0')):
        if name.startswith(SKIP):
            continue
        if a.staged:
            data = git('show', ':' + name, binary=True)
        else:
            path = ROOT / name
            if path.is_symlink() or not path.is_file():
                continue
            data = path.read_bytes()
        if b'\0' in data:
            continue
        for line, placeholder in private_values.findings(data.decode('utf-8', errors='replace'), rules):
            print(f'{name}:{line}: contains {placeholder}')
            found += 1
    if found:
        sys.exit(f'{found} private value(s) or credential(s) found. Remove them before committing; '
                 '`python3 scripts/redact-tracked.py --dry-run` lists affected tracked files.')
    print('No private values or credentials found.')


if __name__ == '__main__':
    main()
