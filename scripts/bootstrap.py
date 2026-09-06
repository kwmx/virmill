#!/usr/bin/env python3
"""Explicit local toolchain bootstrap; never executes an installation script."""
import hashlib
import json
from pathlib import Path
import tarfile
import urllib.request
import urllib.parse
root = Path(__file__).resolve().parents[1]
lock = json.loads((root/'contracts/dependencies.lock.json').read_text())['go']
tools = root/'.tools'
tools.mkdir(exist_ok=True)
archive = tools/(lock['version']+'.linux-amd64.tar.gz')
if not archive.exists():
    with urllib.request.urlopen(lock['source'], timeout=60) as response:
        final = urllib.parse.urlparse(response.url)
        if final.scheme != 'https' or final.hostname not in ('go.dev','dl.google.com'):
            raise SystemExit('Unapproved toolchain redirect')
        with archive.open('xb') as output:
            total = 0
            while chunk := response.read(1 << 20):
                total += len(chunk)
                if total > 256 << 20:
                    raise SystemExit('Toolchain download exceeded limit')
                output.write(chunk)
if hashlib.sha256(archive.read_bytes()).hexdigest() != lock['sha256']:
    raise SystemExit('Toolchain SHA-256 mismatch; nothing extracted')
if (tools/'go').exists():
    raise SystemExit('Existing toolchain preserved; verify/remove explicitly before replacement')
with tarfile.open(archive) as source:
    source.extractall(tools, filter='data')
print('Verified toolchain extracted to repository .tools/go')
