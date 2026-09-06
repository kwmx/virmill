#!/usr/bin/env python3
"""Run a bounded check and append observed evidence; never promote requirement status."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
p = argparse.ArgumentParser()
p.add_argument('--id', required=True)
p.add_argument('--class', dest='evidence_class', required=True)
p.add_argument('--requirements', default='')
p.add_argument('--fixtures', default='')
p.add_argument('--timeout', type=int, default=120)
p.add_argument('--cwd', default='.')
p.add_argument('command', nargs=argparse.REMAINDER)
a = p.parse_args()
command = a.command[1:] if a.command and a.command[0] == '--' else a.command
if not command:
    p.error('command required')
if not all(c.isalnum() or c in '-_' for c in a.id):
    p.error('invalid evidence ID')
files = []
for top in ('cmd', 'internal', 'sdk', 'schemas', 'contracts', 'scripts', 'tests', 'packaging'):
    for f in sorted((ROOT / top).rglob('*')):
        if f.is_file() and '__pycache__' not in str(f):
            files.append((f.relative_to(ROOT).as_posix(), hashlib.sha256(f.read_bytes()).hexdigest()))
for name in ('go.mod', 'go.sum'):
    files.append((name, hashlib.sha256((ROOT / name).read_bytes()).hexdigest()))
digest = hashlib.sha256(json.dumps(files, separators=(',', ':')).encode()).hexdigest()
at = datetime.datetime.now(datetime.timezone.utc).isoformat()
try:
    result = subprocess.run(command, cwd=ROOT / a.cwd, capture_output=True, text=True, timeout=a.timeout)
    code, output = result.returncode, result.stdout + result.stderr
except subprocess.TimeoutExpired as e:
    code, output = 124, 'Check timed out; no passing evidence.\n'
logs = ROOT / 'docs/evidence/logs'
logs.mkdir(exist_ok=True)
log = logs / (a.id + '.log')
if log.exists():
    sys.exit('Evidence ID already exists; choose a new ID to preserve history.')
log.write_text(output)
entry = dict(id=a.id, at=at, revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip(),
             sourceDigest=digest, evidenceClass=a.evidence_class, command=command, cwd=a.cwd,
             exitCode=code, result='passed' if code == 0 else 'failed',
             requirements=[s for s in a.requirements.split(',') if s], environment='environment.json',
             log=log.relative_to(ROOT/'docs/evidence').as_posix(), logSHA256=hashlib.sha256(log.read_bytes()).hexdigest())
entry['fixtureDigests'] = {name: hashlib.sha256((ROOT/name).read_bytes()).hexdigest() for name in a.fixtures.split(',') if name}
if 'SKIP' in output or '[no test files]' in output or 'BLOCKED' in output:
    entry['limitations'] = 'Review log for skipped/unavailable paths; command success is not acceptance of those paths.'
with (ROOT/'docs/evidence/ledger.jsonl').open('a') as f:
    f.write(json.dumps(entry,sort_keys=True)+'\n')
print(output, end='')
print(json.dumps({'evidenceID':a.id,'exitCode':code,'sourceDigest':digest}))
sys.exit(code)
