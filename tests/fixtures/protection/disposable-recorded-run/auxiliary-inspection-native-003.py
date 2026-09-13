"""Single-use parent-operated auxiliary metadata fixture; authoring is not execution.

Only --self-test is approved for local authoring. Normal mode requires all pins
and explicit review/exclusive policy-window switches; see the adjacent recipe.
"""
import argparse
import base64
import copy
import hashlib
import json
import os
import pathlib
import re
import selectors
import sqlite3
import stat
import subprocess
import sys
import time
import unittest
import uuid
import xml.etree.ElementTree as ET

ROOT = pathlib.Path('<test-vm-home>/virmill-tests/run-65930c6-20260907')
RECIPE = 'auxiliary-inspection-native-003'
STATE_ROOT = '/var/lib/virmill-host-helper/auxiliary-fixture-003'
ROOT_ID = 'auxiliary-fixture-003'
VM_NAME = 'virmill-auxiliary-fixture-003'
URI = 'qemu:///system'
POLICY = '/etc/virmill/helper-policy.json'
MAX_COMMAND = 2 << 20
TABLES = {
    'plans': 'id,digest,body,input', 'jobs': 'id,plan_id,body',
    'metadata': 'kind,id,body', 'events': 'job_id,seq,body',
    'locks': 'resource,job_id', 'dedup': 'key,request_digest,job_id,created_at',
}

def require(condition, message):
    if not condition:
        raise RuntimeError(message)

def sha(raw):
    return hashlib.sha256(raw).hexdigest()

def strict_json(raw):
    require(len(raw) <= 8 << 20, 'JSON exceeds bound')

    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate JSON key')
            result[key] = value
        return result

    def constant(_):
        raise ValueError('non-finite JSON constant')

    return json.loads(raw, object_pairs_hook=pairs, parse_constant=constant)

def canonical_uuid(value):
    require(isinstance(value, str) and str(uuid.UUID(value)) == value and uuid.UUID(value).int != 0,
            'noncanonical UUID')
    return value

def generation(st):
    return st.st_dev, st.st_ino, st.st_size, st.st_mtime_ns, st.st_ctime_ns

def read_regular(path, limit=8 << 20, proc=False):
    flags = os.O_RDONLY | os.O_NONBLOCK | os.O_CLOEXEC
    if not proc:
        flags |= os.O_NOFOLLOW
    with os.fdopen(os.open(path, flags), 'rb') as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and before.st_size <= limit, 'input is not a bounded regular file')
        raw = stream.read(limit + 1)
        require(len(raw) <= limit and generation(before) == generation(os.fstat(stream.fileno())),
                'input changed during bounded read')
        return raw

def stop_child(process):
    """Only this unreaped Popen child, never a process group/coordinator/guest."""
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=1)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=2)

def collect_child(arguments, env, timeout=30, limit=MAX_COMMAND):
    output = [bytearray(), bytearray()]
    process = subprocess.Popen(arguments, env=env, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               start_new_session=True, close_fds=True)
    selector = selectors.DefaultSelector()
    deadline = time.monotonic() + timeout
    failure = None
    try:
        for number, stream in enumerate((process.stdout, process.stderr)):
            selector.register(stream, selectors.EVENT_READ, number)
        while selector.get_map():
            require(time.monotonic() < deadline, 'child output/EOF timeout')
            for event, _ in selector.select(min(.05, max(0, deadline - time.monotonic()))):
                block = os.read(event.fileobj.fileno(), 65536)
                if not block:
                    selector.unregister(event.fileobj)
                    continue
                remaining = limit - sum(map(len, output))
                output[event.data].extend(block[:remaining])
                require(len(block) < remaining, 'child stdout/stderr ceiling reached')
        process.wait(timeout=max(.001, deadline - time.monotonic()))
    except BaseException as error:
        failure = error
    finally:
        stop_child(process)
        selector.close()
        process.stdout.close()
        process.stderr.close()
    return process.returncode, bytes(output[0]), bytes(output[1]), failure


def xml_tree(raw):
    require(isinstance(raw, bytes) and 0 < len(raw) <= 1 << 20, 'XML exceeds bound')
    require(b'<!DOCTYPE' not in raw and b'<!ENTITY' not in raw, 'XML declarations unsupported')
    return ET.fromstring(raw)


def uuid_list(raw):
    values = raw.decode('ascii').split()
    require(len(values) <= 64 and len(values) == len(set(values)), 'UUID inventory ambiguous/excessive')
    return sorted(canonical_uuid(value) for value in values)


def volume_list(raw):
    require(len(raw) <= 65536, 'volume list exceeds bound')
    lines = [line.strip() for line in raw.decode('utf-8').splitlines() if line.strip()]
    require(len(lines) >= 2 and lines[0].split() == ['Name', 'Path'] and
            re.fullmatch('-+', lines[1]), 'unrecognized virsh Name/Path table')
    result = {}
    for line in lines[2:]:
        parts = line.split()
        require(len(parts) == 2 and re.fullmatch('[A-Za-z0-9._-]{1,255}', parts[0]),
                'volume name or column shape unsupported')
        name, path = parts
        require(name not in result and path not in result.values() and path.startswith('/') and
                os.path.normpath(path) == path and len(path) <= 4096, 'duplicate/ambiguous volume path')
        result[name] = path
    require(len(result) <= 128, 'volume count exceeds bound')
    return result


def pool_config(raw):
    """Exact bytes except allocation/available values; capacity remains fixed."""
    tree = xml_tree(raw)
    require(tree.tag == 'pool' and tree.get('type') == 'dir', 'only observed directory pools supported')
    numbers, spans = {}, []
    for name in ('capacity', 'allocation', 'available'):
        nodes = tree.findall(name)
        require(len(nodes) == 1 and nodes[0].attrib == {'unit': 'bytes'} and not list(nodes[0]),
                'one direct byte-valued pool statistic required')
        value = nodes[0].text
        require(value is not None and re.fullmatch('[0-9]{1,20}', value), 'invalid pool statistic')
        numbers[name] = int(value)
        require(numbers[name] <= (1 << 64) - 1, 'pool integer exceeds bound')
        matches = list(re.finditer(rb'<' + name.encode() + rb'\b[^<>]*>([0-9]+)</' + name.encode() + rb'>', raw))
        require(len(matches) == 1 and matches[0][1].decode() == value, 'ambiguous pool statistic bytes')
        if name != 'capacity':
            spans.append(matches[0].span(1))
    require(numbers['capacity'] > 0 and numbers['allocation'] + numbers['available'] == numbers['capacity'],
            'pool accounting bounds/sum differ')
    for start, end in sorted(spans, reverse=True):
        raw = raw[:start] + b'OBSERVED_COUNTER' + raw[end:]
    return raw


def volume_config(raw):
    """Preserve every byte except the one validated native access timestamp."""
    tree = xml_tree(raw)
    nodes = tree.findall('./target/timestamps/atime')
    require(tree.tag == 'volume' and tree.get('type') == 'file' and
            len(nodes) == len(tree.findall('.//atime')) == 1,
            'one file-volume access timestamp required')
    node = nodes[0]
    require(not node.attrib and not list(node) and isinstance(node.text, str) and
            re.fullmatch('[0-9]{1,20}(?:\\.[0-9]{1,9})?', node.text), 'invalid volume access timestamp')
    matches = list(re.finditer(rb'<atime>([0-9]+(?:\.[0-9]+)?)</atime>', raw))
    require(len(matches) == 1 and matches[0][1].decode() == node.text,
            'ambiguous volume access timestamp bytes')
    start, end = matches[0].span(1)
    return raw[:start] + b'OBSERVED_ACCESS_TIME' + raw[end:], node.text


def policy_variant(original, identity, vm, state_uid, state_gid, grant):
    p = copy.deepcopy(original)
    require(set(p) <= {'apiVersion', 'keys', 'roots', 'actors', 'auxiliary'} and
            p['apiVersion'] == 'virmill/v1' and isinstance(p['roots'], dict) and
            isinstance(p.get('auxiliary', []), list), 'unknown policy contract')
    require(ROOT_ID not in p['roots'] and STATE_ROOT not in p['roots'].values(), 'fixture root already granted')
    require(identity['actorUID'] in p['actors'] and p['keys'].get(identity['keyID']) == identity['publicKey'],
            'original policy does not already approve this actor/public key')
    require(all(g['resourceID'] != vm and g['rootID'] != ROOT_ID for g in p.get('auxiliary', [])),
            'fixture grant overlaps an existing permission')
    p['roots'][ROOT_ID] = STATE_ROOT
    if grant:
        p.setdefault('auxiliary', []).append({
            'actorUID': identity['actorUID'], 'keyID': identity['keyID'], 'resourceID': vm,
            'rootID': ROOT_ID, 'stateUID': state_uid, 'stateGID': state_gid,
            'maxBytes': 8192, 'maxMembers': 3, 'allowCapture': False,
        })
    return p


def same_policy(a, b):
    left, right = copy.deepcopy(a), copy.deepcopy(b)
    # Expected public-policy reads may update atime. Every other observed
    # content/identity/access field remains part of the compare.
    left['stat'].pop('atimeNS', None)
    right['stat'].pop('atimeNS', None)
    return left == right


def response_record(code, out, err, expected=None):
    require(out.endswith(b'\n') and out.count(b'\n') == 1, 'unclean machine response')
    response = strict_json(out)
    require(isinstance(response, dict) and response.get('apiVersion') == 'virmill/v1', 'wrong response contract')
    if expected is None:
        require(code == 0 and err == b'' and response.get('error') is None and response.get('data') is not None,
                'inspection/identity command failed')
    else:
        require(code == {'INVALID_INPUT': 2, 'UNSUPPORTED_CAPABILITY': 3, 'PERMISSION_DENIED': 4}[expected] and response.get('data') is None and isinstance(response.get('error'), dict) and
                response['error'].get('code') == expected, 'expected refusal absent or successful data retained')
        message = response['error'].get('message')
        require(isinstance(message, str) and message and '\n' not in message and '\r' not in message and
                err == (expected + ': ' + message + '\n').encode('utf-8'),
                'stderr differs from the typed CLI refusal diagnostic')
    return response


def validate_inventory(response, vm, fingerprint, state_uid, state_gid):
    data = response['data']
    require(all(data.get(k) is False for k in
                ('captureVerified', 'independentRestoreVerified', 'guestBootVerified')),
            'metadata promoted to recovery proof')
    observation = data['observation']
    require(observation['version'] == 1 and observation['stage'] == 'inspected' and
            observation.get('artifact') is None, 'unexpected captured artifact/stage')
    canonical_uuid(observation['jobID'])
    require(re.fullmatch('[a-f0-9]{64}', observation['binding']), 'invalid response binding')
    inventory = observation['inventory']
    require(inventory['version'] == 1 and inventory['resource'] == {
        'providerID': 'libvirt', 'connectionID': URI, 'kind': 'vm', 'resourceUUID': vm} and
        inventory['fingerprint'] == fingerprint and inventory['root']['id'] == ROOT_ID and
        inventory['root']['path'] == STATE_ROOT and inventory['root']['state']['uid'] == 0,
        'inventory identity differs')
    require(inventory['layout']['vmID'] == vm and
            inventory['layout']['firmware']['nvram']['path'] == STATE_ROOT + '/nvram.fd' and
            inventory['layout']['tpm']['sourcePath'] == STATE_ROOT + '/tpm' and
            inventory['layout']['tpm']['sourceType'] == 'dir', 'native source mapping differs')
    expected = {'nvram.fd': ('nvram', 4096), 'tpm/empty.state': ('tpm', 0), 'tpm/ordinary.state': ('tpm', 32)}
    members = inventory['members']
    require(len(members) == 3 and len({m['relativePath'] for m in members}) == 3 and
            len({m['id'] for m in members}) == 3 and inventory['totalBytes'] == 4128, 'payload set/count differs')
    for member in members:
        require(member['relativePath'] in expected and
                (member['kind'], member['state']['size']) == expected[member['relativePath']], 'member shape differs')
    lock = inventory['tpmLock']
    require(lock['kind'] == 'tpm-lock' and lock['relativePath'] == 'tpm/.lock' and lock['state']['size'] == 0,
            'empty producer control file was omitted or counted as payload')
    for member in members + [lock]:
        state = member['state']
        require(state['uid'] == state_uid and state['gid'] == state_gid and state['links'] == 1 and
                state['mode'] == stat.S_IFREG | 0o600 and isinstance(state['generation'], str) and
                bool(state['generation']), 'member access/generation metadata differs')
    return inventory


def fixture_xml(vm, emulator):
    canonical_uuid(vm)
    require(emulator == '/usr/bin/qemu-system-x86_64', 'unreviewed emulator path')
    return f'''<domain type='kvm'>
  <name>{VM_NAME}</name><uuid>{vm}</uuid>
  <memory unit='MiB'>128</memory><currentMemory unit='MiB'>128</currentMemory><vcpu>1</vcpu>
  <os><type arch='x86_64' machine='pc-q35-10.2'>hvm</type>
    <loader readonly='yes' secure='no' type='pflash' format='raw' stateless='no'>{STATE_ROOT}/code.fd</loader>
    <nvram format='raw'>{STATE_ROOT}/nvram.fd</nvram>
  </os>
  <features><acpi/><apic/></features>
  <on_poweroff>destroy</on_poweroff><on_reboot>destroy</on_reboot><on_crash>destroy</on_crash>
  <devices><emulator>{emulator}</emulator>
    <tpm model='tpm-crb'><backend type='emulator' version='2.0' persistent_state='yes'>
      <source type='dir' path='{STATE_ROOT}/tpm'/>
    </backend></tpm>
  </devices>
</domain>\n'''.encode()


# This fixed program is sent only by a parent-executed normal run. It is not a
# product helper endpoint, accepts no arbitrary shell or private-key path, and
# never opens any auxiliary payload for reading. Self-tests compile it only.
ROOT_PROGRAM = r'''
import base64, copy, hashlib, json, os, pathlib, stat, sys
ROOT = '/var/lib/virmill-host-helper/auxiliary-fixture-003'
POLICY = '/etc/virmill/helper-policy.json'
DISPOSABLE = '<test-vm-home>/virmill-tests/run-65930c6-20260907'
def need(ok, message):
    if not ok: raise RuntimeError(message)
def signature(s):
    return dict(dev=s.st_dev, ino=s.st_ino, mode=s.st_mode, uid=s.st_uid, gid=s.st_gid,
                links=s.st_nlink, size=s.st_size, atimeNS=s.st_atime_ns, mtimeNS=s.st_mtime_ns, ctimeNS=s.st_ctime_ns)
def same_policy(a,b):
    a,b=copy.deepcopy(a),copy.deepcopy(b)
    a['stat'].pop('atimeNS',None); b['stat'].pop('atimeNS',None)
    return a==b
def attrs(fd):
    names=sorted(os.listxattr(fd)); need(len(names)<=32, 'xattr count')
    result={n:os.getxattr(fd,n).hex() for n in names}
    need(sum(len(n)+len(v)//2 for n,v in result.items())<=8192, 'xattr bytes')
    return result
def directory(path):
    need(path.startswith('/') and os.path.normpath(path)==path, 'directory path')
    fd=os.open('/',os.O_PATH|os.O_DIRECTORY|os.O_CLOEXEC)
    try:
        for name in path.split('/')[1:]:
            new=os.open(name,os.O_PATH|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC,dir_fd=fd)
            os.close(fd); fd=new
        return fd
    except BaseException: os.close(fd); raise
def snapshot_policy():
    parent=directory('/etc/virmill')
    try:
        ps=os.fstat(parent); need(ps.st_uid==0 and not ps.st_mode&0o022, 'unsafe policy parent')
        fd=os.open('helper-policy.json',os.O_RDONLY|os.O_NONBLOCK|os.O_NOFOLLOW|os.O_NOATIME|os.O_CLOEXEC,dir_fd=parent)
        try:
            before=os.fstat(fd)
            need(stat.S_ISREG(before.st_mode) and before.st_nlink==1 and before.st_uid==0 and
                 not before.st_mode&0o022 and before.st_size<=16384, 'unsafe/oversize public policy')
            raw=os.read(fd,16385); need(len(raw)==before.st_size, 'incomplete public policy')
            x=attrs(fd); after=os.fstat(fd)
            need(signature(before)==signature(after)==signature(os.stat('helper-policy.json',dir_fd=parent,follow_symlinks=False)), 'policy changed')
            return {'bytes':base64.b64encode(raw).decode(),'stat':signature(after),'xattrs':x}
        finally: os.close(fd)
    finally: os.close(parent)
def metadata(path):
    parent=directory(os.path.dirname(path))
    try:
        fd=os.open(os.path.basename(path),os.O_PATH|os.O_NOFOLLOW|os.O_CLOEXEC,dir_fd=parent)
        try:
            before=os.fstat(fd); need(not stat.S_ISLNK(before.st_mode), 'symlink metadata source')
            return signature(before)
        finally: os.close(fd)
    finally: os.close(parent)
def fixture_meta():
    result={}
    def walk(path,depth):
        need(depth<=8 and len(result)<=64,'fixture tree bound')
        result[path[len(ROOT):] or '.']=metadata(path)
        if stat.S_ISDIR(result[path[len(ROOT):] or '.']['mode']):
            fd=os.open(path,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_NOATIME|os.O_CLOEXEC)
            try: names=sorted(os.listdir(fd))
            finally: os.close(fd)
            need(len(names)<=64,'fixture names bound')
            for name in names: walk(path+'/'+name,depth+1)
    walk(ROOT,0); return result
need(os.getuid()==0,'root administration subprogram requires sudo')
need(len(sys.argv)==2 and len(sys.argv[1])<=112*1024,'bounded fixed request required')
r=json.loads(sys.argv[1]); action=r['action']
if action=='policy-read':
    result=snapshot_policy()
elif action=='policy-replace':
    need(set(r)=={'action','expected','replacement','originalMetadata','phase'},'policy request fields')
    need(r['phase'] in ('root-only','grant','restore'),'policy phase')
    need(same_policy(snapshot_policy(),r['expected']),'policy compare failed; preserve without replacement')
    raw=base64.b64decode(r['replacement'],validate=True); need(len(raw)<=16384,'replacement bound')
    json.loads(raw)
    original=r['originalMetadata']; need(original['stat']['uid']==0,'original policy owner')
    parent=directory('/etc/virmill'); name='.auxiliary-fixture-003-'+r['phase']
    fd=None
    try:
        fd=os.open(name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW|os.O_CLOEXEC,0o600,dir_fd=parent)
        with os.fdopen(os.dup(fd),'wb') as f: f.write(raw); f.flush()
        os.fchown(fd,original['stat']['uid'],original['stat']['gid'])
        os.fchmod(fd,stat.S_IMODE(original['stat']['mode']))
        for n in os.listxattr(fd):
            if n not in original['xattrs']: os.removexattr(fd,n)
        for n,value in original['xattrs'].items(): os.setxattr(fd,n,bytes.fromhex(value))
        os.utime(fd,ns=(original['stat']['atimeNS'],original['stat']['mtimeNS']))
        os.fsync(fd)
        s=os.fstat(fd)
        need(s.st_mode==original['stat']['mode'] and s.st_uid==original['stat']['uid'] and
             s.st_gid==original['stat']['gid'] and attrs(fd)==original['xattrs'],'replacement metadata differs')
        need(same_policy(snapshot_policy(),r['expected']),'policy changed before atomic replacement; temp retained')
        os.rename(name,'helper-policy.json',src_dir_fd=parent,dst_dir_fd=parent)
        sync=os.open('.',os.O_RDONLY|os.O_DIRECTORY,dir_fd=parent)
        try: os.fsync(sync)
        finally: os.close(sync)
        result=snapshot_policy()
        need(result['bytes']==r['replacement'] and result['stat']['ino']==s.st_ino and
             result['xattrs']==original['xattrs'],'policy changed after replacement; preserve')
    finally:
        if fd is not None: os.close(fd)
        os.close(parent)
elif action=='create':
    need(set(r)=={'action','uid','gid'},'creation fields')
    need(type(r['uid']) is int and type(r['gid']) is int and 0<r['uid']<2**32-1 and 0<r['gid']<2**32-1,'state ownership')
    parent=directory(os.path.dirname(ROOT)); p=os.fstat(parent)
    need(p.st_uid==0 and not p.st_mode&0o022,'unsafe helper state parent')
    os.mkdir(os.path.basename(ROOT),0o700,dir_fd=parent); os.close(parent)
    os.mkdir(ROOT+'/tpm',0o700); os.chown(ROOT+'/tpm',r['uid'],r['gid'])
    for name,payload in [('code.fd',b'ORIGINAL-DUMMY-CODE\0'*256),('nvram.fd',b'\0'*4096),
                         ('tpm/.lock',b''),('tpm/empty.state',b''),('tpm/ordinary.state',b'original-generated-state-only!!!')]:
        fd=os.open(ROOT+'/'+name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW|os.O_CLOEXEC,0o600)
        try:
            need(os.write(fd,payload)==len(payload),'generated write incomplete')
            os.fchown(fd,r['uid'],r['gid']); os.fsync(fd)
        finally: os.close(fd)
    result=fixture_meta()
elif action=='fixture-meta': result=fixture_meta()
elif action=='prior-helper-meta':
    result={}; base=os.path.dirname(ROOT)
    def prior_walk(path,depth):
        need(depth<=16 and len(result)<=4096,'helper journal metadata bound')
        s=metadata(path); result[path]=s
        if stat.S_ISDIR(s['mode']):
            fd=os.open(path,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_NOATIME|os.O_CLOEXEC)
            try: names=sorted(os.listdir(fd))
            finally: os.close(fd)
            need(len(names)<=4096,'helper directory bound')
            for name in names:
                if path+'/'+name!=ROOT: prior_walk(path+'/'+name,depth+1)
    prior_walk(base,0)
    # Creating this recipe's one child necessarily changes the parent directory.
    result.pop(base); result={p:s for p,s in result.items()}
elif action=='fifo-add':
    need(set(r)=={'action','expected','uid','gid'} and fixture_meta()==r['expected'],'fixture changed before fault')
    os.mkfifo(ROOT+'/tpm/refusal.fifo',0o600); os.chown(ROOT+'/tpm/refusal.fifo',r['uid'],r['gid'])
    result=fixture_meta()
elif action=='fifo-remove':
    need(set(r)=={'action','expected'} and fixture_meta()==r['expected'],'fault fixture changed; preserve')
    need(stat.S_ISFIFO(r['expected']['/tpm/refusal.fifo']['mode']),'wrong fault object')
    os.unlink(ROOT+'/tpm/refusal.fifo'); result=fixture_meta()
elif action=='media':
    path=r['path']; need(isinstance(path,str) and os.path.normpath(path)==path and
        (path.startswith(DISPOSABLE+'/sources/') or path.startswith(DISPOSABLE+'/data/') or
         path.startswith('/var/lib/libvirt/images/')),'disk/media source outside approved roots')
    result={'stat':metadata(path)}; need(stat.S_ISREG(result['stat']['mode']) and result['stat']['links']==1 and
        0<result['stat']['size']<=16<<40,'media metadata bound')
    # Public EFI artifact and tiny declared images only; no firmware/TPM path.
    if r['hash']:
        need(result['stat']['size']<=64<<20,'selected public disk hash exceeds bound')
        fd=os.open(path,os.O_RDONLY|os.O_NONBLOCK|os.O_NOFOLLOW|os.O_NOATIME|os.O_CLOEXEC)
        try:
            need(signature(os.fstat(fd))==result['stat'],'media changed before hash')
            h=hashlib.sha256(); total=0
            while True:
                b=os.read(fd,1<<20)
                if not b: break
                total+=len(b); need(total<=64<<20,'media grew during hash'); h.update(b)
            need(total==result['stat']['size'] and signature(os.fstat(fd))==result['stat']==metadata(path),'media changed during hash')
            result['sha256']=h.hexdigest()
        finally: os.close(fd)
elif action=='process-hash':
    need(type(r['pid']) is int and r['pid']>1,'process PID')
    path='/proc/'+str(r['pid'])+'/exe'; target=os.readlink(path)
    need(target=='/usr/libexec/virmill-host-helper','unexpected helper executable')
    fd=os.open(path,os.O_RDONLY|os.O_NOATIME|os.O_CLOEXEC)
    try:
        s=os.fstat(fd); need(stat.S_ISREG(s.st_mode) and s.st_size<=128<<20,'helper executable bound')
        h=hashlib.sha256(); total=0
        while True:
            b=os.read(fd,1<<20)
            if not b:break
            total+=len(b); need(total<=128<<20,'helper executable grew'); h.update(b)
        need(signature(s)==signature(os.fstat(fd)),'helper executable changed')
        result={'pid':r['pid'],'sha256':h.hexdigest()}
    finally:os.close(fd)
else: raise RuntimeError('administration action is not allowlisted')
print(json.dumps(result,separators=(',',':')))
'''


class Run:
    def __init__(self, args):
        self.args, self.started, self.sequence = args, time.monotonic(), 0
        self.final_started = None
        self.command_bytes = 0
        self.output = ROOT / RECIPE
        self.host_env = {**os.environ, 'LC_ALL': 'C', 'LANG': 'C', 'PATH': '/usr/bin:/usr/sbin:/bin:/sbin'}
        self.env = dict(self.host_env)
        self.stage = 'preflight'
        self.original_policy = self.current_policy = None
        self.policy_uncertain = False
        self.baseline = self.guests = self.pools = self.media = self.prior_files = self.helper_files = None
        self.created = False
        self.native_xml = None
        self.report = {'recipe': RECIPE, 'revision': args.revision, 'sourceDigest': None,
                       'deploymentManifestSHA256': args.deployment_sha256, 'recipeSHA256': args.recipe_sha256,
                       'expectedBinarySHA256': args.binary_sha256, 'status': 'uncertain-preserve-resources',
                       'checks': [], 'failures': [], 'policyRestored': False, 'guestStarted': False,
                       'auxiliaryPayloadReadByRecipe': False, 'validFirmwareOrTPMState': False,
                       'captureVerified': False, 'independentRestoreVerified': False, 'guestBootVerified': False,
                       'actualTUIObserved': False, 'staleNativeRequestInjected': False}

    def save(self, name, value):
        raw = value if isinstance(value, bytes) else (json.dumps(value, sort_keys=True, indent=2) + '\n').encode()
        with (self.output / name).open('xb') as f:
            f.write(raw); f.flush(); os.fsync(f.fileno())

    def command(self, args, timeout=30, cleanup=False):
        if self.final_started is not None:
            require(time.monotonic() - self.final_started < 300, 'five-minute final observation budget exceeded')
        else:
            require(cleanup or time.monotonic() - self.started < 1200, '20-minute recipe budget exceeded')
        self.sequence += 1
        require(self.sequence <= 2048, 'command count exceeds fixture bound')
        env = self.env if args[0] == '/usr/bin/virmill' else self.host_env
        code, out, err, failure = collect_child(args, env, timeout)
        self.command_bytes += len(out) + len(err)
        require(self.command_bytes <= 32 << 20, 'aggregate command output exceeds fixture bound')
        # Public policy is allowed in restricted output; no private key bytes
        # enter this fixture. The large fixed administration program is hashed.
        logged = args
        if args[:4] == ['/usr/bin/sudo', '-n', '/usr/bin/python3', '-c']:
            logged = args[:4] + ['sha256:' + sha(args[4].encode()), strict_json(args[5])]
        self.save('command-%03d.json' % self.sequence,
                  {'stage': self.stage, 'arguments': logged, 'exitCode': code,
                   'stdout': out.decode('utf-8', errors='backslashreplace'), 'stderr': err.decode('utf-8', errors='backslashreplace'),
                   'stdoutBase64': base64.b64encode(out).decode(), 'stderrBase64': base64.b64encode(err).decode(),
                   'collectionError': str(failure) if failure else None})
        if failure:
            raise RuntimeError('bounded child failed: ' + str(failure))
        return code, out, err

    def checked(self, args, timeout=30, cleanup=False):
        code, out, err = self.command(args, timeout, cleanup)
        require(code == 0 and not err, 'command failed; inspect command-%03d.json' % self.sequence)
        return out

    def root(self, action, cleanup=False, **fields):
        request = json.dumps({'action': action, **fields}, separators=(',', ':'))
        require(len(request.encode()) <= 112 << 10, 'fixed administration request exceeds bound')
        raw = self.checked(['/usr/bin/sudo', '-n', '/usr/bin/python3', '-c', ROOT_PROGRAM, request], cleanup=cleanup)
        return strict_json(raw)

    def virsh(self, *args, write=False):
        # The only writes are explicit define of this new, never-started UUID.
        if write:
            require(args[0] == 'define' and len(args) == 3 and args[1] == '--validate' and
                    pathlib.Path(args[2]).parent == self.output, 'native write outside fixture define')
        return self.checked(['/usr/bin/virsh', *([] if write else ['--readonly']), '-c', URI, *args])

    def cli(self, *args, expected=None, output='json'):
        code, out, err = self.command(['/usr/bin/virmill', *args, '--connection', URI,
                                       '--output', output, '--non-interactive'])
        return response_record(code, out, err, expected)

    def inspect(self, filename, expected=None, output='json'):
        value = self.cli('vm', 'recovery', 'auxiliary', 'inspect', self.vm, '--input',
                         json.dumps({'rootID': ROOT_ID}), expected=expected, output=output)
        self.save(filename, value)
        return value

    def property(self, unit, key, user=False):
        return self.checked(['/usr/bin/systemctl', *(['--user'] if user else []), 'show',
                             unit, '-p', key, '--value']).decode().strip()

    def runtime(self):
        manifest_bytes = read_regular(ROOT / 'packages' / self.args.revision[:7] / 'deployment.json')
        require(sha(manifest_bytes) == self.args.deployment_sha256, 'deployment hash differs')
        manifest = strict_json(manifest_bytes)
        require(manifest['revision'] == self.args.revision and
                re.fullmatch('[a-f0-9]{64}', manifest['sourceDigest']) and
                re.fullmatch('[a-f0-9]{64}', manifest['sourceInventorySHA256']), 'deployment source pin differs')
        binaries = {}
        for name, path in {'virmill': '/usr/bin/virmill', 'virmilld': '/usr/bin/virmilld',
                           'virmill-host-helper': '/usr/libexec/virmill-host-helper'}.items():
            expected = manifest['artifacts']['build/bin/' + name]
            require(re.fullmatch('[a-f0-9]{64}', expected), 'invalid deployed binary digest')
            binaries[name] = sha(read_regular(path, 128 << 20))
            require(binaries[name] == expected, 'installed binary differs: ' + name)
        require(binaries['virmill'] == self.args.binary_sha256, 'explicit CLI binary pin differs')
        unit = '<test-vm-login>-' + self.args.revision[:7] + '.service'
        require(self.property(unit, 'ActiveState', True) == 'active' and
                self.property(unit, 'WorkingDirectory', True) == str(ROOT), 'private coordinator unavailable')
        pid = self.property(unit, 'MainPID', True)
        require(re.fullmatch('[1-9][0-9]*', pid), 'invalid coordinator PID')
        require(sha(read_regular('/proc/' + pid + '/exe', 128 << 20, proc=True)) == binaries['virmilld'] and
                self.property(unit, 'MainPID', True) == pid, 'running coordinator differs')
        # Parent must start these separately. No socket-triggered activation is
        # used as a substitute for checking the installed running helper.
        helper_unit = 'virmill-host-helper.service'
        require(self.property(helper_unit, 'ActiveState') == 'active' and
                self.property('virmill-host-helper.socket', 'ActiveState') == 'active', 'helper not prepared by parent')
        helper_pid = self.property(helper_unit, 'MainPID')
        require(re.fullmatch('[1-9][0-9]*', helper_pid), 'invalid helper PID')
        running = self.root('process-hash', pid=int(helper_pid))
        require(running['sha256'] == binaries['virmill-host-helper'] and
                self.property(helper_unit, 'MainPID') == helper_pid, 'running helper differs')
        require(self.cli('version')['data']['revision'] == self.args.revision, 'reported revision differs')
        current = {'unit': unit, 'pid': pid, 'helperPID': helper_pid, 'binarySHA256': binaries}
        require('runtime' not in self.report or self.report['runtime'] == current, 'runtime changed during fixture')
        self.report.update(runtime=current, sourceDigest=manifest['sourceDigest'],
                           sourceInventorySHA256=manifest['sourceInventorySHA256'])

    def journal(self):
        db = ROOT / 'state/virmill/journal.db'
        require(db.is_file() and db.resolve() == db, 'journal path is not the approved regular file')
        connection = sqlite3.connect(db.as_uri() + '?mode=ro', uri=True, timeout=2)
        total = size = 0
        result = {}
        try:
            connection.execute('PRAGMA query_only=ON'); connection.execute('BEGIN')
            for table, columns in TABLES.items():
                rows = []
                for row in connection.execute('SELECT ' + columns + ' FROM ' + table + ' ORDER BY ' + columns):
                    total += 1
                    encoded = [({'bytesHex': v.hex()} if isinstance(v, bytes) else v) for v in row]
                    payload = json.dumps(encoded, separators=(',', ':')).encode()
                    size += len(payload)
                    require(total <= 100000 and size <= 32 << 20, 'journal snapshot exceeds bound')
                    rows.append(sha(payload))
                result[table] = rows
            result['schema'] = list(connection.execute("SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY type,name"))
            require(len(result['schema']) <= 256 and not result['locks'], 'existing locks or excessive schema')
            require(not any(row[0] == 'trigger' for row in result['schema']), 'pre-existing journal triggers')
            return result
        finally:
            connection.close()

    def stopped_xml(self, vm):
        require(self.virsh('domstate', vm).strip() == b'shut off', 'domain is not stopped: ' + vm)
        info = {}
        for line in self.virsh('dominfo', vm).decode().splitlines():
            if ':' in line:
                key, value = line.split(':', 1)
                require(key not in info, 'duplicate dominfo field')
                info[key] = value.strip()
        require(info['Persistent'] == 'yes' and info['Autostart'] == 'disable' and info['Managed save'] == 'no',
                'persistent/autostart/managed-save boundary differs')
        raw = self.virsh('dumpxml', '--inactive', vm)
        require(xml_tree(raw).findtext('uuid') == vm, 'native XML UUID differs')
        return raw

    def pool_snapshot(self):
        result, total = {}, 0
        for pool in uuid_list(self.virsh('pool-list', '--all', '--uuid')):
            raw = self.virsh('pool-dumpxml', pool)
            tree = xml_tree(raw)
            require(tree.findtext('uuid') == pool, 'pool XML identity differs')
            names = volume_list(self.virsh('vol-list', '--pool', pool))
            total += len(names); require(total <= 128, 'total volumes exceed fixture bound')
            volumes, access_times = {}, {}
            for name in sorted(names):
                volumes[name], access_times[name] = volume_config(self.virsh('vol-dumpxml', '--pool', pool, name))
            self.report.setdefault('observedVolumeAccessTimes', []).append({'pool': pool, 'values': access_times})
            self.report['volumeXMLAccessTimeExcludedFromEquality'] = True
            result[pool] = {'configuration': pool_config(raw), 'names': names, 'volumes': volumes}
        return result

    def domain_capabilities(self):
        # This read query is forbidden on libvirt read-only connections. The
        # fixed command uses a normal connection but performs no host mutation.
        return self.checked(['/usr/bin/virsh', '-c', URI, 'domcapabilities',
                             '--virttype', 'kvm', '--arch', 'x86_64', '--machine', 'pc-q35-10.2'])

    def disk_paths(self):
        paths = {str(ROOT / 'sources/uefi-tpm-probe-v1/probe-fat.img')}
        for raw in self.guests.values():
            for disk in xml_tree(raw).findall('./devices/disk'):
                source = disk.find('source')
                if source is None:
                    continue
                require(disk.get('device') in ('disk', 'cdrom'), 'unsupported existing disk class')
                if disk.get('type') == 'file':
                    require(set(source.attrib) <= {'file', 'index'} and source.get('file'), 'ambiguous file source')
                    paths.add(source.get('file'))
                elif disk.get('type') == 'volume':
                    require(set(source.attrib) <= {'pool', 'volume', 'index'}, 'ambiguous volume source')
                    paths.add(self.virsh('vol-path', '--pool', source.attrib['pool'], source.attrib['volume']).decode().strip())
                else:
                    raise RuntimeError('unsupported existing disk source; fixture refuses before mutation')
        require(len(paths) <= 64, 'source/media inventory exceeds bound')
        return sorted(paths)

    def files_snapshot(self):
        # Existing disposable files are stat-only, including any signing-key
        # pathname. Database rows have their separate transactional comparison.
        out = {}
        for base, prefix in ((ROOT, 'run/'), (pathlib.Path('<test-vm-home>/images'), 'source-media/')):
            require(base.is_dir() and base.resolve() == base, 'preserved media/run root unavailable')
            for directory, dirs, files in os.walk(base, followlinks=False):
                dirs[:] = sorted(d for d in dirs if pathlib.Path(directory, d) != self.output)
                require(len(dirs) + len(files) <= 4096, 'disposable directory exceeds bound')
                for name in sorted(dirs + files):
                    path = pathlib.Path(directory, name)
                    if path.parent == ROOT / 'state/virmill' and path.name in ('journal.db', 'journal.db-wal', 'journal.db-shm'):
                        continue
                    s = path.lstat()
                    out[prefix + str(path.relative_to(base))] = [s.st_dev, s.st_ino, s.st_mode, s.st_uid, s.st_gid,
                                                       s.st_size, s.st_mtime_ns, s.st_ctime_ns]
                    require(len(out) <= 20000, 'disposable file inventory exceeds bound')
        return out

    def preserve(self):
        if self.baseline is not None:
            require(self.journal() == self.baseline, 'pre-existing journal rows/schema/locks changed')
            self.report['allJournalRowsAndSchemaPreserved'] = True
        if self.guests is not None:
            expected = set(self.guests) | ({self.vm} if self.created else set())
            for vm, raw in self.guests.items():
                require(self.stopped_xml(vm) == raw, 'pre-existing domain XML changed: ' + vm)
            self.report['allEarlierStoppedXMLPreserved'] = True
            current_ids = uuid_list(self.virsh('list', '--all', '--uuid'))
            self.report['observedDomainUUIDs'] = current_ids
            require(set(current_ids) == expected, 'domain set changed')
            if self.created:
                raw = self.stopped_xml(self.vm)
                if self.native_xml is not None:
                    require(raw == self.native_xml, 'new fixture XML differs from last confirmed definition')
                self.report['finalFixtureXMLSHA256'] = sha(raw)
        if self.pools is not None:
            require(self.pool_snapshot() == self.pools, 'existing pool configuration/volume inventory changed')
            self.report['allPoolConfigurationAndVolumesPreserved'] = True
        if self.media is not None:
            for path, earlier in self.media.items():
                require(self.root('media', path=path, hash='sha256' in earlier) == earlier,
                        'existing disk/media metadata or selected bytes changed: ' + path)
            self.report['allDeclaredDiskMetadataAndSelectedHashesPreserved'] = True
        if self.prior_files is not None:
            require(self.files_snapshot() == self.prior_files, 'prior disposable file metadata changed')
            self.report['priorDisposableFileMetadataPreserved'] = True
            self.report['suppliedSourceMediaMetadataPreserved'] = True
        if self.helper_files is not None:
            require(self.root('prior-helper-meta') == self.helper_files, 'prior helper journal metadata changed')
            self.report['priorHelperFileMetadataPreserved'] = True

    def replace_policy(self, phase, raw):
        require(not self.policy_uncertain and self.current_policy is not None, 'unknown current policy; preserve')
        # Mark uncertain before invoking a mutation. Lost command acknowledgement
        # never authorizes automatic replay or restoration of an unknown file.
        self.policy_uncertain = True
        changed = self.root('policy-replace', cleanup=phase == 'restore', expected=self.current_policy,
                            replacement=base64.b64encode(raw).decode(), originalMetadata=self.original_policy,
                            phase=phase)
        self.current_policy = changed
        self.policy_uncertain = False
        self.save('policy-' + phase + '-receipt.json', changed)

    def define(self, name, raw):
        tree = xml_tree(raw)
        require(tree.findtext('uuid') == self.vm and tree.findtext('name') == VM_NAME and
                not tree.findall('./devices/disk') and not tree.findall('./devices/interface'), 'fixture XML scope differs')
        if self.created or self.native_xml is not None:
            require(self.created and self.native_xml is not None,
                    'previous definition acknowledgement uncertain; preserve without overwrite')
            require(self.stopped_xml(self.vm) == self.native_xml,
                    'new fixture definition changed before replacement; preserve without overwrite')
        path = self.output / name
        self.save(name, raw)
        # Intent is local to the disposable recipe, not a Virmill operation.
        self.created = True
        self.native_xml = None
        self.virsh('define', '--validate', str(path), write=True)
        self.native_xml = self.stopped_xml(self.vm)
        return self.native_xml

    def preflight(self):
        require(os.getuid() > 0 and self.checked(['/usr/bin/id', '-un']).strip() == b'<test-vm-login>',
                'only the designated ordinary disposable actor may execute')
        env = strict_json(read_regular(ROOT / 'environment.json'))
        for key in ('XDG_STATE_HOME', 'XDG_RUNTIME_DIR', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_CONFIG_HOME'):
            path = pathlib.Path(env[key])
            require(path.is_absolute() and path.is_relative_to(ROOT) and path.resolve() == path and path.is_dir(),
                    'private environment directory is absent or escapes disposable root')
            self.env[key] = str(path)
        require(pathlib.Path(self.env['XDG_STATE_HOME']) == ROOT / 'state', 'private journal selection differs')
        self.runtime()
        self.identity = self.cli('host', 'helper', 'identity')['data']
        require(self.identity['actorUID'] == os.getuid() and self.identity['policyApproved'] is True and
                self.identity['policyPath'] == POLICY and self.identity['socketPath'] == '/run/virmill-host-helper/control.sock' and
                re.fullmatch('[a-f0-9]{64}', self.identity['keyID']) and
                re.fullmatch('[a-f0-9]{64}', self.identity['publicKey']) and
                sha(bytes.fromhex(self.identity['publicKey'])) == self.identity['keyID'], 'public helper identity differs')
        self.save('public-helper-identity.json', self.identity)
        self.original_policy = self.current_policy = self.root('policy-read')
        self.save('original-public-policy.json', self.original_policy)
        self.original_bytes = base64.b64decode(self.original_policy['bytes'], validate=True)
        self.original_decoded = strict_json(self.original_bytes)
        passwd = self.checked(['/usr/bin/getent', 'passwd', 'qemu']).decode().strip().split(':')
        group = self.checked(['/usr/bin/getent', 'group', 'qemu']).decode().strip().split(':')
        require(len(passwd) == 7 and len(group) == 4 and passwd[0] == group[0] == 'qemu' and
                passwd[2].isdigit() and passwd[3].isdigit() and group[2].isdigit() and
                passwd[3] == group[2], 'observed qemu account/group mapping is unsupported')
        self.state_uid, self.state_gid = int(passwd[2]), int(group[2])
        require(0 < self.state_uid < 2**32-1 and 0 < self.state_gid < 2**32-1, 'qemu identity bound')
        self.report['fixtureStateAccount'] = {'user': 'qemu', 'group': 'qemu', 'uid': self.state_uid, 'gid': self.state_gid}
        self.vm = canonical_uuid(str(uuid.uuid4()))
        self.report.update(vmID=self.vm, vmName=VM_NAME, fixtureRoot=STATE_ROOT)
        self.baseline = self.journal()
        ids = uuid_list(self.virsh('list', '--all', '--uuid'))
        require(bool(ids) and self.vm not in ids, 'existing domain set absent or UUID collision')
        names = self.virsh('list', '--all', '--name').decode().splitlines()
        require(VM_NAME not in names, 'fixture domain name already exists; never reuse it')
        self.guests = {vm: self.stopped_xml(vm) for vm in ids}
        self.save('baseline-journal.json', self.baseline)
        self.save('baseline-guests.json', {vm: sha(raw) for vm, raw in self.guests.items()})
        for vm, raw in self.guests.items():
            self.save('baseline-' + vm + '.xml', raw)
        self.pools = self.pool_snapshot()
        self.save('baseline-pools.json', {vm: {
            'configurationSHA256': sha(p['configuration']), 'volumes': {n: sha(v) for n, v in p['volumes'].items()},
            'names': p['names']} for vm, p in self.pools.items()})
        paths = self.disk_paths()
        self.media = {}
        for path in paths:
            value = self.root('media', path=path, hash=False)
            # Hash only original public EFI media and the already declared tiny
            # copied probe disks, never all older multi-GB images.
            selected = path == str(ROOT / 'sources/uefi-tpm-probe-v1/probe-fat.img') or (
                path.startswith('/var/lib/libvirt/images/virmill-cold-probe-v1/virmill-') and
                re.fullmatch('virmill-[a-f0-9-]{36}-disk-000.qcow2', pathlib.Path(path).name))
            self.media[path] = self.root('media', path=path, hash=True) if selected else value
        self.save('baseline-media.json', self.media)
        self.report['olderLargeDiskBytesRehashed'] = False
        self.prior_files = self.files_snapshot()
        self.helper_files = self.root('prior-helper-meta')
        self.save('baseline-prior-files.json', self.prior_files)
        self.save('baseline-helper-files.json', self.helper_files)
        capabilities = xml_tree(self.domain_capabilities())
        require(capabilities.findtext('arch') == 'x86_64' and capabilities.findtext('machine') == 'pc-q35-10.2',
                'native capability architecture/machine differs')
        self.emulator = capabilities.findtext('path')
        self.root_only = policy_variant(self.original_decoded, self.identity, self.vm, self.state_uid, self.state_gid, False)
        self.granted = policy_variant(self.original_decoded, self.identity, self.vm, self.state_uid, self.state_gid, True)
        self.save('review-root-only-policy.json', self.root_only)
        self.save('review-metadata-only-policy.json', self.granted)
        self.save('review-new-domain.xml', fixture_xml(self.vm, self.emulator))
        self.preserve()
        self.report['checks'].append('preflight pins, existing stopped XML, public identity and preservation baseline')

    def execute(self):
        self.preflight()
        self.stage = 'new generated state and never-started definition'
        initial = self.root('create', uid=self.state_uid, gid=self.state_gid)
        self.save('generated-state-metadata.json', initial)
        require(initial['/tpm/ordinary.state']['size'] == 32 and initial['/nvram.fd']['size'] == 4096,
                'generated fixture sizes differ from reviewed metadata')
        original_xml = self.define('define-original.xml', fixture_xml(self.vm, self.emulator))
        self.save('native-original.xml', original_xml)
        observed = xml_tree(original_xml)
        source = observed.findall('./devices/tpm/backend/source')
        require(len(source) == 1 and source[0].attrib == {'type': 'dir', 'path': STATE_ROOT + '/tpm'} and
                observed.findtext('./os/nvram') == STATE_ROOT + '/nvram.fd' and
                observed.find('./os/nvram').get('format') == 'raw', 'libvirt did not preserve explicit fixture paths')
        require(self.root('fixture-meta') == initial, 'definition changed dummy state metadata')
        self.inspect('denied-original-policy.json', expected='PERMISSION_DENIED')
        self.stage = 'root-only policy refusal'
        self.replace_policy('root-only', (json.dumps(self.root_only, sort_keys=True) + '\n').encode())
        public = self.cli('host', 'helper', 'identity')['data']
        require(public['policyApproved'] is True and public['roots'].get(ROOT_ID) == STATE_ROOT,
                'root-only refusal would be masked by base identity/root failure')
        self.inspect('denied-root-only-policy.json', expected='PERMISSION_DENIED')
        require(self.root('fixture-meta') == initial, 'denied request changed fixture metadata')
        self.report['checks'].append('original policy and base actor/key/root without auxiliary permission deny inspection')
        self.stage = 'exact metadata grant and JSON/NDJSON inspection'
        self.replace_policy('grant', (json.dumps(self.granted, sort_keys=True) + '\n').encode())
        native = self.cli('vm', 'recovery', 'inspect', self.vm)['data']
        require(native['state'] == 'stopped' and native['persistent'] is True and
                native['hasManagedSave'] is False and native['autostart'] is False,
                'new native observation is not the required cold state')
        self.save('native-inspection.json', native)
        first = self.inspect('inspection-json.json')
        inventory = validate_inventory(first, self.vm, native['fingerprint'], self.state_uid, self.state_gid)
        require(inventory['layout'] == native['layout'], 'coordinator/helper native layout differs')
        second = self.inspect('inspection-ndjson.json', output='ndjson')
        require(validate_inventory(second, self.vm, native['fingerprint'], self.state_uid, self.state_gid) == inventory and
                second['data']['observation']['jobID'] != first['data']['observation']['jobID'],
                'repeat metadata differs or read correlation was reused')
        require(self.root('fixture-meta') == initial, 'metadata reads changed generated objects')
        self.report['checks'].append('exact metadata-only grant returns three payload members and separate empty TPM lock in JSON/NDJSON')
        self.stage = 'new FIFO refusal'
        fault = self.root('fifo-add', expected=initial, uid=self.state_uid, gid=self.state_gid)
        self.save('fifo-fault-metadata.json', fault)
        refused = self.inspect('denied-special-file.json', expected='UNSUPPORTED_CAPABILITY')
        require('auxiliary objects must be directories or single-link regular files' in refused['error']['message'],
                'FIFO refusal came from an unrelated capability or member budget')
        require(self.root('fixture-meta') == fault, 'special-file refusal changed fixture metadata')
        restored_files = self.root('fifo-remove', expected=fault)
        require({p: s for p, s in initial.items() if stat.S_ISREG(s['mode'])} ==
                {p: s for p, s in restored_files.items() if stat.S_ISREG(s['mode'])}, 'original generated members changed')
        self.report['checks'].append('new FIFO refused without blocking or successful inventory; only exact created FIFO removed')
        self.stage = 'explicitly unresolved TPM source refusal'
        unresolved = xml_tree(original_xml)
        backend = unresolved.find('./devices/tpm/backend')
        backend.remove(backend.find('source'))
        raw_unresolved = self.define('define-unresolved.xml', ET.tostring(unresolved, encoding='utf-8'))
        require(not xml_tree(raw_unresolved).findall('./devices/tpm/backend/source'),
                'native normalization unexpectedly resolved TPM source')
        self.save('native-unresolved.xml', raw_unresolved)
        # Frozen 490b88c rejects the missing native path at auxiliaryRelative as
        # INVALID_INPUT. This is a configuration-support refusal, not a bad UUID.
        refused = self.inspect('denied-unresolved-source.json', expected='INVALID_INPUT')
        require('canonical bounded native auxiliary paths required' in refused['error']['message'],
                'unresolved-source refusal came from an unrelated invalid input')
        require(self.root('fixture-meta') == restored_files, 'unresolved-source refusal changed dummy state')
        self.stage = 'restore only the new explicit definition'
        require(self.define('define-restored.xml', original_xml) == original_xml, 'new explicit definition did not restore exactly')
        final = self.inspect('inspection-restored.json')
        validate_inventory(final, self.vm, native['fingerprint'], self.state_uid, self.state_gid)
        require(self.root('fixture-meta') == restored_files, 'restored metadata read changed dummy state')
        self.report['checks'].append('unresolved source refused without guessed path; original new definition restored exactly')
        self.stage = 'restore exact original public policy'
        self.replace_policy('restore', self.original_bytes)
        self.verify_policy_restored()
        self.inspect('denied-restored-original-policy.json', expected='PERMISSION_DENIED')
        self.runtime()
        self.preserve()
        self.report['checks'].append('original public policy restored; old inventory preserved; new domain remains stopped and uninitialized')

    def verify_policy_restored(self):
        actual = self.root('policy-read', cleanup=True)
        require(same_policy(actual, self.current_policy), 'policy changed after recorded replacement')
        original = self.original_policy
        require(actual['bytes'] == original['bytes'] and actual['xattrs'] == original['xattrs'] and
                all(actual['stat'][k] == original['stat'][k] for k in
                    ('mode', 'uid', 'gid', 'size', 'links', 'mtimeNS')), 'policy content/access metadata not restored')
        self.report['policyRestored'] = True
        self.report['restoredPolicySHA256'] = sha(base64.b64decode(actual['bytes']))

    def finish(self):
        # Policy is the only automatic failure cleanup. Never redefine, unlink
        # generated state, retry an inspection, or remove a guest on failure.
        self.report['lastStage'] = self.stage
        self.final_started = time.monotonic()
        if self.original_policy is not None:
            try:
                require(not self.policy_uncertain, 'policy publication acknowledgement uncertain; manual compare required')
                if self.current_policy['bytes'] != self.original_policy['bytes']:
                    self.replace_policy('restore', self.original_bytes)
                self.verify_policy_restored()
            except BaseException as error:
                self.report['failures'].append('policy restoration: ' + str(error))
                self.report['policyRestored'] = False
        # Observe each independent earlier resource class even when another
        # class is uncertain. No class's success makes the overall run pass.
        fields = ('baseline', 'guests', 'pools', 'media', 'prior_files', 'helper_files')
        saved = {field: getattr(self, field) for field in fields}
        for field in fields:
            setattr(self, field, None)
        for field in fields:
            setattr(self, field, saved[field])
            try:
                self.preserve()
            except BaseException as error:
                self.report['failures'].append('final ' + field + ' observation: ' + str(error))
            finally:
                setattr(self, field, None)
        for field in fields:
            setattr(self, field, saved[field])
        self.report['retention'] = {'domainUUID': getattr(self, 'vm', None), 'domainName': VM_NAME,
                                    'stateRoot': STATE_ROOT, 'output': str(self.output),
                                    'action': 'retain all newly created resources for parent review; no blind retry or cleanup'}
        self.report['commandCount'] = self.sequence
        if not self.report['failures'] and self.report['policyRestored']:
            self.report['status'] = 'passed'
        self.save('report.json', self.report)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--self-test', action='store_true')
    for name in ('revision', 'deployment-sha256', 'binary-sha256', 'recipe-sha256'):
        parser.add_argument('--' + name)
    parser.add_argument('--execute-reviewed', action='store_true')
    parser.add_argument('--exclusive-policy-window', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        require(not any((args.revision, args.deployment_sha256, args.binary_sha256, args.recipe_sha256,
                         args.execute_reviewed, args.exclusive_policy_window)), 'self-test cannot carry native execution arguments')
        result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SelfTests))
        return 0 if result.wasSuccessful() else 1
    require(args.execute_reviewed and args.exclusive_policy_window, 'parent review and exclusive administrator policy window required')
    require(re.fullmatch('[a-f0-9]{40}', args.revision or ''), 'exact frozen revision required')
    for value in (args.deployment_sha256, args.binary_sha256, args.recipe_sha256):
        require(re.fullmatch('[a-f0-9]{64}', value or ''), 'exact deployment/binary/recipe SHA256 required')
    require(sha(read_regular(pathlib.Path(__file__), 1 << 20)) == args.recipe_sha256, 'uploaded recipe hash differs')
    require(ROOT.is_dir() and ROOT.resolve() == ROOT, 'designated disposable root absent or symlinked')
    os.umask(0o077)
    runner = Run(args)
    runner.output.mkdir(mode=0o700)  # exclusive, never reuse an earlier attempt
    try:
        runner.execute()
    except BaseException as error:
        runner.report['failures'].append(type(error).__name__ + ': ' + str(error))
    finally:
        runner.finish()
    print(json.dumps(runner.report, sort_keys=True))
    return 0 if runner.report['status'] == 'passed' else 1


class SelfTests(unittest.TestCase):
    def test_strict_json(self):
        for raw in (b'{"x":1,"x":2}', b'{"n":NaN}', b'{}{}', b'{', b' ' * ((8 << 20) + 1)):
            with self.subTest(raw=raw[:32]), self.assertRaises((RuntimeError, ValueError)):
                strict_json(raw)
        self.assertEqual(strict_json(b'{"x":[]}'), {'x': []})

    def test_uuid_inventory(self):
        vm = '12345678-1234-4234-8234-123456789abc'
        self.assertEqual(uuid_list((vm + '\n\n').encode()), [vm])
        for raw in ((vm + '\n' + vm).encode(), vm.upper().encode(), b'00000000-0000-0000-0000-000000000000', b'name'):
            with self.subTest(raw=raw), self.assertRaises((RuntimeError, ValueError)):
                uuid_list(raw)

    def test_volume_table(self):
        raw = b' Name Path\n--------------------\n image.qcow2 /var/lib/libvirt/images/image.qcow2\n\n'
        self.assertEqual(volume_list(raw), {'image.qcow2': '/var/lib/libvirt/images/image.qcow2'})
        self.assertEqual(volume_list(b'Name Path\n------------\n'), {})
        for bad in (raw.replace(b'Name', b'Future'), raw.replace(b'---', b'===', 1),
                    raw + b'image.qcow2 /another\n', raw + b'other /var/lib/libvirt/images/image.qcow2\n',
                    raw.replace(b'image.qcow2 /', b'bad name /'), raw.replace(b'/var/lib/', b'/var/../lib/'),
                    raw.replace(b' /var', b' relative/var')):
            with self.subTest(raw=bad), self.assertRaises(RuntimeError):
                volume_list(bad)

    def test_pool_numeric_drift_only(self):
        raw = b"<pool type='dir'><name>old</name><capacity unit='bytes'>100</capacity><allocation unit='bytes'>40</allocation><available unit='bytes'>60</available><target><path>/existing</path></target></pool>"
        changed = raw.replace(b'>40<', b'>45<').replace(b'>60<', b'>55<')
        self.assertEqual(pool_config(raw), pool_config(changed))
        self.assertNotEqual(pool_config(raw), pool_config(raw.replace(b'/existing', b'/other')))
        for bad in (raw.replace(b'>40<', b'>-1<'), raw.replace(b'>60<', b'>61<'),
                    raw.replace(b"unit='bytes'", b"unit='KiB'", 1),
                    raw.replace(b'</pool>', b"<capacity unit='bytes'>100</capacity></pool>"),
                    raw.replace(b'<pool ', b'<!DOCTYPE pool><pool ', 1)):
            with self.subTest(raw=bad), self.assertRaises((RuntimeError, ET.ParseError)):
                pool_config(bad)

    def test_volume_access_time_only_is_excluded(self):
        raw = b"<volume type='file'><capacity unit='bytes'>40</capacity><allocation unit='bytes'>20</allocation><target><path>/old</path><timestamps><atime>123.123456789</atime><mtime>123.1</mtime><ctime>123.2</ctime><btime>0</btime></timestamps></target></volume>"
        normalized, observed = volume_config(raw)
        self.assertEqual(observed, '123.123456789')
        self.assertEqual(normalized, volume_config(raw.replace(b'123.123456789', b'125.1'))[0])
        for before, after in ((b'/old', b'/new'), (b'>40<', b'>41<'), (b'>20<', b'>21<'),
                              (b'<mtime>123.1', b'<mtime>124.1'), (b'<ctime>123.2', b'<ctime>124.2')):
            self.assertNotEqual(normalized, volume_config(raw.replace(before, after))[0])
        for bad in (raw.replace(b'123.123456789', b'-1'), raw.replace(b'123.123456789', b'1.1234567890'),
                    raw.replace(b'<atime>', b'<atime unit="seconds">'), raw.replace(b'</volume>', b'<atime>4</atime></volume>'),
                    raw.replace(b'<atime>123.123456789</atime>', b''), raw.replace(b'<atime>', b'<atime><child/>')):
            with self.subTest(raw=bad), self.assertRaises((RuntimeError, ET.ParseError)):
                volume_config(bad)

    def test_capabilities_uses_only_fixed_read_query_on_normal_connection(self):
        runner = object.__new__(Run)
        calls = []
        runner.checked = lambda args: calls.append(args) or b'<domainCapabilities/>'
        self.assertEqual(runner.domain_capabilities(), b'<domainCapabilities/>')
        self.assertEqual(calls, [['/usr/bin/virsh', '-c', URI, 'domcapabilities',
                                 '--virttype', 'kvm', '--arch', 'x86_64', '--machine', 'pc-q35-10.2']])

    def test_policy_scope_and_preservation(self):
        identity = {'actorUID': 1000, 'keyID': 'a' * 64, 'publicKey': 'b' * 64}
        original = {'apiVersion': 'virmill/v1', 'actors': [1000, 1001], 'keys': {'a' * 64: 'b' * 64},
                    'roots': {'old': '/unchanged'}, 'auxiliary': []}
        vm = '12345678-1234-4234-8234-123456789abc'
        root = policy_variant(original, identity, vm, 107, 107, False)
        grant = policy_variant(original, identity, vm, 107, 107, True)
        self.assertEqual(root['auxiliary'], [])
        self.assertEqual(len(grant['auxiliary']), 1)
        self.assertFalse(grant['auxiliary'][0]['allowCapture'])
        self.assertEqual(grant['auxiliary'][0]['resourceID'], vm)
        self.assertEqual(grant['auxiliary'][0]['maxBytes'], 8192)
        for field in ('actors', 'keys'):
            self.assertEqual(original[field], grant[field])
        self.assertEqual(original['roots'], {'old': '/unchanged'})
        for bad in (root, {**original, 'future': True}, {**original, 'actors': [1001]}):
            with self.subTest(policy=bad), self.assertRaises(RuntimeError):
                policy_variant(bad, identity, vm, 107, 107, True)

    def test_policy_compare_access_and_generation(self):
        original = {'bytes': 'e30=', 'stat': {'ino': 1, 'ctimeNS': 2, 'mtimeNS': 3, 'atimeNS': 4,
                    'mode': 0o100640, 'uid': 0, 'gid': 0}, 'xattrs': {'security.selinux': '6162', 'system.posix_acl_access': '6364'}}
        read = copy.deepcopy(original); read['stat']['atimeNS'] += 1
        self.assertTrue(same_policy(original, read))
        for field in original['stat']:
            if field == 'atimeNS':
                continue
            bad = copy.deepcopy(original); bad['stat'][field] += 1
            with self.subTest(field=field): self.assertFalse(same_policy(original, bad))
        for field in original['xattrs']:
            bad = copy.deepcopy(original); bad['xattrs'][field] = '00'
            with self.subTest(xattr=field): self.assertFalse(same_policy(original, bad))
        bad = copy.deepcopy(original); bad['bytes'] = 'W10='
        self.assertFalse(same_policy(original, bad))

    def test_machine_refusal(self):
        good = b'{"apiVersion":"virmill/v1","data":{"observation":{}}}\n'
        self.assertIsNotNone(response_record(0, good, b'')['data'])
        denied = b'{"apiVersion":"virmill/v1","error":{"code":"PERMISSION_DENIED","message":"not approved"}}\n'
        self.assertEqual(response_record(4, denied, b'PERMISSION_DENIED: not approved\n', 'PERMISSION_DENIED')['error']['code'], 'PERMISSION_DENIED')
        for code, raw, err in ((0, denied, b''), (4, good, b''), (4, denied, b'noise'),
                               (4, denied + b'\n', b''), (4, denied.replace(b'"error":', b'"data":{},"error":'), b'')):
            with self.subTest(raw=raw, code=code), self.assertRaises(RuntimeError):
                response_record(code, raw, err, 'PERMISSION_DENIED')

    def test_generated_xml_scope(self):
        vm = '12345678-1234-4234-8234-123456789abc'
        tree = xml_tree(fixture_xml(vm, '/usr/bin/qemu-system-x86_64'))
        self.assertEqual(tree.findtext('uuid'), vm)
        self.assertEqual(tree.findtext('name'), VM_NAME)
        self.assertEqual(tree.findall('./devices/disk') + tree.findall('./devices/interface'), [])
        self.assertEqual(tree.find('./devices/tpm/backend/source').attrib, {'type': 'dir', 'path': STATE_ROOT + '/tpm'})
        self.assertEqual(tree.findtext('./os/nvram'), STATE_ROOT + '/nvram.fd')
        with self.assertRaises(RuntimeError): fixture_xml(vm, '/unreviewed/emulator')

    def test_redefine_refuses_drift_or_uncertain_previous_acknowledgement(self):
        vm = '12345678-1234-4234-8234-123456789abc'
        original = fixture_xml(vm, '/usr/bin/qemu-system-x86_64')
        runner = object.__new__(Run)
        runner.vm, runner.created, runner.native_xml = vm, True, original
        runner.stopped_xml = lambda _: original.replace(b'<vcpu>1</vcpu>', b'<vcpu>2</vcpu>')
        runner.save = lambda *_: self.fail('drift must refuse before saving or defining')
        runner.virsh = lambda *_args, **_kw: self.fail('drift must not redefine')
        with self.assertRaisesRegex(RuntimeError, 'changed before replacement'):
            runner.define('replacement.xml', original)
        self.assertEqual(runner.native_xml, original)
        runner.native_xml = None
        runner.stopped_xml = lambda _: self.fail('unknown definition must not be treated as observed')
        with self.assertRaisesRegex(RuntimeError, 'acknowledgement uncertain'):
            runner.define('replacement.xml', original)

    def test_inventory_exact_payload_and_no_proof(self):
        vm, fingerprint = '12345678-1234-4234-8234-123456789abc', 'a' * 64
        def member(path, kind, size, identifier):
            return {'id': identifier, 'kind': kind, 'relativePath': path, 'state': {
                'uid': 107, 'gid': 107, 'links': 1, 'mode': stat.S_IFREG | 0o600, 'generation': 'synthetic', 'size': size}}
        inventory = {'version': 1, 'resource': {'providerID': 'libvirt', 'connectionID': URI, 'kind': 'vm', 'resourceUUID': vm},
            'fingerprint': fingerprint, 'root': {'id': ROOT_ID, 'path': STATE_ROOT, 'state': {'uid': 0}},
            'layout': {'vmID': vm, 'firmware': {'nvram': {'path': STATE_ROOT + '/nvram.fd'}},
                       'tpm': {'sourcePath': STATE_ROOT + '/tpm', 'sourceType': 'dir'}},
            'members': [member('nvram.fd', 'nvram', 4096, 'members/000'), member('tpm/empty.state', 'tpm', 0, 'members/001'),
                        member('tpm/ordinary.state', 'tpm', 32, 'members/002')],
            'totalBytes': 4128, 'tpmLock': member('tpm/.lock', 'tpm-lock', 0, 'tpm-lock')}
        response = {'data': {'captureVerified': False, 'independentRestoreVerified': False, 'guestBootVerified': False,
                            'observation': {'version': 1, 'stage': 'inspected', 'jobID': vm, 'binding': 'b'*64, 'inventory': inventory}}}
        self.assertEqual(validate_inventory(response, vm, fingerprint, 107, 107), inventory)
        changes = [lambda d: d.update(captureVerified=True),
                   lambda d: d['observation'].update(artifact={}),
                   lambda d: d['observation']['inventory']['resource'].update(resourceUUID='other'),
                   lambda d: d['observation']['inventory']['members'].pop(),
                   lambda d: d['observation']['inventory']['members'][1].update(relativePath='nvram.fd'),
                   lambda d: d['observation']['inventory']['members'][0]['state'].update(uid=0),
                   lambda d: d['observation']['inventory']['tpmLock']['state'].update(size=1)]
        for index, change in enumerate(changes):
            bad = copy.deepcopy(response); change(bad['data'])
            with self.subTest(index=index), self.assertRaises(RuntimeError):
                validate_inventory(bad, vm, fingerprint, 107, 107)

    def test_embedded_administration_compiles_without_execution(self):
        compile(ROOT_PROGRAM, '<root-administration-source-not-executed>', 'exec')
        self.assertEqual(len(b'original-generated-state-only!!!'), 32)
        self.assertIn("need(same_policy(snapshot_policy(),r['expected'])", ROOT_PROGRAM)
        self.assertNotIn('PRIVATE KEY', ROOT_PROGRAM)

    def test_argv_environment_boundary(self):
        # Pure seam: systemctl/virsh/sudo must keep host XDG_RUNTIME_DIR;
        # only installed Virmill CLI receives the private coordinator values.
        from unittest.mock import patch
        args = argparse.Namespace(revision='a'*40, deployment_sha256='b'*64, binary_sha256='c'*64, recipe_sha256='d'*64)
        runner = Run(args); runner.env['XDG_RUNTIME_DIR'] = str(ROOT / 'runtime')
        runner.host_env['XDG_RUNTIME_DIR'] = '/run/user/1000'
        runner.save = lambda *_: None
        with patch.dict(globals(), {'collect_child': lambda argv, env, timeout: (0, env['XDG_RUNTIME_DIR'].encode(), b'', None)}):
            self.assertEqual(runner.command(['/usr/bin/systemctl', '--user', 'show'])[1], b'/run/user/1000')
            self.assertEqual(runner.command(['/usr/bin/virmill', 'version'])[1], str(ROOT / 'runtime').encode())

    def test_uncertain_policy_acknowledgement_never_replays_or_restores(self):
        args = argparse.Namespace(revision='a'*40, deployment_sha256='b'*64, binary_sha256='c'*64, recipe_sha256='d'*64)
        runner = Run(args)
        runner.original_policy = {'bytes': 'e30='}
        runner.current_policy = {'bytes': 'W10='}
        runner.policy_uncertain = True
        runner.root = lambda *a, **kw: self.fail('uncertain policy must not invoke administration again')
        saved = {}
        runner.save = lambda name, value: saved.update({name: value})
        runner.finish()
        report = saved['report.json']
        self.assertEqual(report['status'], 'uncertain-preserve-resources')
        self.assertFalse(report['policyRestored'])
        self.assertIn('acknowledgement uncertain', report['failures'][0])

    def test_native_execution_requires_review_and_all_pins(self):
        from unittest.mock import patch
        for arguments in ([], ['--execute-reviewed', '--exclusive-policy-window'], ['--self-test', '--execute-reviewed']):
            with self.subTest(arguments=arguments), patch.object(sys, 'argv', ['fixture', *arguments]), self.assertRaises(RuntimeError):
                main()


if __name__ == '__main__':
    sys.exit(main())
