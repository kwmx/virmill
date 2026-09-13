#!/usr/bin/env python3
"""Replace private values in tracked text files and record the change.

Each changed file's before/after SHA-256 is appended to the evidence ledger, so
logs whose hashes older entries recorded stay verifiable against Git history.
"""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import private_values

ROOT = private_values.ROOT
LEDGER = ROOT / 'docs/evidence/ledger.jsonl'
# Upstream vendor source and the preserved specification package stay byte-exact.
SKIP = ('vendor/', 'virmill-v1-spec/')


def main():
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument('--id', help='ledger record ID for this redaction')
    p.add_argument('--dry-run', action='store_true', help='list files that would change')
    a = p.parse_args()
    if not a.dry_run and not a.id:
        p.error('--id is required unless --dry-run')
    if not private_values.CONFIG.is_file():
        sys.exit('No .virmill-local/private-values.json; nothing to redact against.')
    if a.id and a.id in {json.loads(line)['id'] for line in LEDGER.read_text().splitlines()}:
        sys.exit('Ledger ID already exists; choose a new ID.')
    rules = private_values.load()
    names = subprocess.run(['git', 'ls-files', '-z'], cwd=ROOT, capture_output=True, check=True, text=True).stdout
    changed = {}
    for name in filter(None, names.split('\0')):
        path = ROOT / name
        if name.startswith(SKIP) or path.is_symlink() or not path.is_file():
            continue
        data = path.read_bytes()
        try:
            text = data.decode('utf-8')
        except UnicodeDecodeError:
            continue
        if '\0' in text:
            continue
        new, count = private_values.redact(text, rules)
        if not count:
            continue
        after = new.encode()
        committed = subprocess.run(['git', 'show', 'HEAD:' + name], cwd=ROOT, capture_output=True).stdout
        changed[name] = {'beforeSHA256': hashlib.sha256(data).hexdigest(),
                         'afterSHA256': hashlib.sha256(after).hexdigest(), 'replacements': count,
                         'beforeMatchesRevision': committed == data}
        if a.dry_run:
            print(f'{name}: {count}')
        else:
            path.write_bytes(after)
    if a.dry_run:
        print(f'{len(changed)} file(s) would change.')
        return
    revision = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=ROOT, capture_output=True, check=True, text=True).stdout.strip()
    entry = {'id': a.id, 'at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'revision': revision,
             'evidenceClass': 'redaction', 'result': 'passed', 'requirements': [], 'files': changed,
             'note': 'Private test-environment values were replaced with placeholders. beforeSHA256 is the content '
                     'just before redaction; where beforeMatchesRevision is true, that is the file at the recorded '
                     'revision in Git history.'}
    with LEDGER.open('a') as f:
        f.write(json.dumps(entry, sort_keys=True) + '\n')
    print(f'{len(changed)} file(s) redacted; recorded {a.id}.')


if __name__ == '__main__':
    main()
