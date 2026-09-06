#!/usr/bin/env python3
"""Compare two offline builds/packages under this exact local native toolchain."""
import hashlib
import json
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parents[1]
def run():
    subprocess.run(['./scripts/build.sh'],cwd=ROOT,check=True,capture_output=True)
    subprocess.run(['python3','scripts/package.py'],cwd=ROOT,check=True,capture_output=True)
    paths=list((ROOT/'build/bin').glob('*'))+list((ROOT/'dist').glob('*.rpm'))+list((ROOT/'dist').glob('*.deb'))
    return {p.relative_to(ROOT).as_posix():hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(paths) if p.is_file()}
first=run()
second=run()
if first != second:
    print(json.dumps({'first':first,'second':second},indent=2))
    raise SystemExit('Reproducibility mismatch; release claim blocked')
print(json.dumps({'identical':True,'nativeEnvironment':'docs/evidence/environment.json','artifacts':second},indent=2))
