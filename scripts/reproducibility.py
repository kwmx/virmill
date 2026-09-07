#!/usr/bin/env python3
"""Compare exact offline build/package outputs under this local native toolchain."""
import hashlib
import json
from pathlib import Path
import subprocess

from package import BINARY_PATHS, PACKAGE_ARTIFACTS, read_regular

ROOT = Path(__file__).resolve().parents[1]


def collect_outputs(root):
    paths = BINARY_PATHS + tuple('dist/' + name for name in PACKAGE_ARTIFACTS)
    return {path: hashlib.sha256(read_regular(root, path)).hexdigest() for path in sorted(paths)}


def run(root=ROOT):
    subprocess.run(['./scripts/build.sh'], cwd=root, check=True, capture_output=True)
    subprocess.run(['python3', 'scripts/package.py'], cwd=root, check=True, capture_output=True)
    return collect_outputs(root)


def main():
    first = run()
    second = run()
    if first != second:
        print(json.dumps({'first': first, 'second': second}, indent=2))
        raise SystemExit('Reproducibility mismatch; release claim blocked')
    print(json.dumps({'identical': True, 'nativeEnvironment': 'docs/evidence/environment.json', 'artifacts': second}, indent=2))


if __name__ == '__main__':
    main()
