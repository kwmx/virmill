#!/usr/bin/env python3
"""One-run recovery of the unattached pair left by native network fixture 003."""
import hashlib
import json
import os
from pathlib import Path
import subprocess

assert os.geteuid() == 0
root = Path('<test-vm-home>/virmill-tests/network-native-003/network-packet-a0cdad29-0f51-461f-85df-44a517b46100')
journal = (root / 'commands.jsonl').read_bytes()
assert hashlib.sha256(journal).hexdigest() == '2e7c744c06e963da8d1a9c7b01cc258897ed138cc5cbf6263934045d95f0f107'
assert not Path('/run/netns/virmill-packet-a0cdad290f51461f85df44a517b46100-a').exists()
names = {'vpaa0cdad290f': (10, '22:22:a5:0b:8a:90', 'veaa0cdad290f'),
         'veaa0cdad290f': (9, 'b2:d4:04:eb:5b:83', 'vpaa0cdad290f')}
events = [json.loads(line) for line in journal.splitlines()]
# udev applied 99-default.link after the first observation. Sequences 19 and 20
# independently recorded the final addresses with the same indices and peers.
recorded = json.loads(next(e['stdout'] for e in events if e.get('phase') == 'result' and e.get('sequence') == 19))
confirmed = json.loads(next(e['stdout'] for e in events if e.get('phase') == 'result' and e.get('sequence') == 20))
def links():
    return json.loads(subprocess.check_output(['/usr/bin/ip', '-j', '-d', 'link', 'show'], timeout=10))
def checked(items):
    selected = {item['ifname']: item for item in items if item['ifname'] in names}
    assert set(selected) == set(names)
    for name, (index, mac, peer) in names.items():
        item = selected[name]
        assert (item['ifindex'], item['address'], item['link']) == (index, mac, peer)
        assert item['linkinfo']['info_kind'] == 'veth' and item['operstate'] == 'DOWN'
        assert 'UP' not in item['flags'] and not item.get('master') and not item.get('ifalias')
    return selected
checked(recorded)
assert checked(confirmed) == checked(recorded)
before = links()
selected = checked(before)
def other_identities(items):
    return {i['ifname']: (i['ifindex'], i['address'], i.get('master')) for i in items if i['ifname'] not in names}
receipt = root / 'parent-pair-cleanup.json'
with receipt.open('x') as f:
    json.dump({'status': 'intent', 'journalSHA256': hashlib.sha256(journal).hexdigest(), 'pair': selected}, f)
    f.flush()
    os.fsync(f.fileno())
checked(links())
subprocess.run(['/usr/bin/ip', 'link', 'delete', 'dev', 'vpaa0cdad290f'], check=True, timeout=10)
after = links()
assert not any(i['ifname'] in names for i in after)
assert other_identities(before) == other_identities(after)
result = {'status': 'verified', 'removedOnlyRecordedUnattachedPair': True, 'otherLinkIdentitiesPreserved': True,
          'journalSHA256': hashlib.sha256(journal).hexdigest()}
with (root / 'parent-pair-cleanup-result.json').open('x') as f:
    json.dump(result, f)
    f.flush()
    os.fsync(f.fileno())
print(json.dumps(result))
