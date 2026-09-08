#!/usr/bin/env python3
"""Run only on the owner-authorized disposable host, using a NEW disk copy.

The supplied source VM, all preexisting definitions and the source disk are
preserved. The generated test VM/disk/journal are retained for examination.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import time
import uuid
import xml.etree.ElementTree as ET


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--execute-disposable', action='store_true', required=True)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--source-vm', required=True)
    args = parser.parse_args()
    root = args.root.resolve(strict=True)
    assert root.is_relative_to(Path.home()/'virmill-tests') and os.getuid() != 0
    assert str(uuid.UUID(args.source_vm)) == args.source_vm
    result_dir = root/'results'
    result_dir.mkdir(mode=0o700)
    events = []
    report = {'status':'failed', 'sourceVM':args.source_vm,
              'scope':'one copied BIOS Kali guest; native event and durable receipt, not OS readiness or full lifecycle qualification'}
    daemon = None
    vmid = str(uuid.uuid4())
    report['testVM'] = vmid
    before = None
    source_path = None
    source_hash = None
    defined = False
    env = dict(os.environ)

    def run(argv, timeout=60, check=True):
        started = time.monotonic()
        p = subprocess.run(argv, capture_output=True, text=True, timeout=timeout, env=env)
        events.append({'argv':argv,'exitCode':p.returncode,'seconds':round(time.monotonic()-started,3),'stdout':p.stdout,'stderr':p.stderr})
        assert len(p.stdout)+len(p.stderr)<2<<20
        (result_dir/'commands.json').write_text(json.dumps(events,indent=2)+'\n')
        if check and p.returncode: raise RuntimeError('command failed: '+repr(argv))
        return p

    def virsh(*argv, **kwargs):
        return run(['virsh','-c','qemu:///system',*argv],**kwargs).stdout

    def inventory():
        found={}
        for ident in virsh('list','--all','--uuid').split():
            if ident==vmid: continue
            assert virsh('domstate',ident).strip()=='shut off'
            xml=virsh('dumpxml',ident,'--inactive')
            found[ident]=hashlib.sha256(xml.encode()).hexdigest()
        return found

    def cli(*argv,check=True,timeout=100):
        p=run([str(root/'bin/virmill'),*argv,'--output','json','--non-interactive'],timeout=timeout,check=check)
        obj=json.loads(p.stdout)
        if check: assert obj['error'] is None,obj
        return obj

    def operation(action):
        plan=cli('vm',action,vmid,'--plan')['data']
        argv=['plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key',str(uuid.uuid4()),'--wait']
        for ack in plan['acknowledgements']: argv.extend(['--ack',ack])
        return plan,cli(*argv,check=False)

    try:
        manifest=json.loads((root/'binaries.json').read_text())
        for name,digest in manifest.items():
            assert name in ('virmill','virmilld')
            assert hashlib.sha256((root/'bin'/name).read_bytes()).hexdigest()==digest
        assert set(manifest)=={'virmill','virmilld'}
        report['binaries']=manifest
        before=inventory(); assert args.source_vm in before
        original=ET.fromstring(virsh('dumpxml',args.source_vm,'--inactive'))
        assert original.find('./os/loader') is None and original.find('./devices/tpm') is None
        disks=original.findall('./devices/disk[@device="disk"]');assert len(disks)==1
        selected=disks[0].find('source');assert set(selected.attrib)=={'pool','volume'}
        source_path=virsh('vol-path','--pool',selected.attrib['pool'],selected.attrib['volume']).strip()
        assert source_path.startswith('/var/lib/libvirt/images/')
        info=json.loads(run(['sudo','-n','qemu-img','info','--output=json',source_path]).stdout)
        assert info['format']=='qcow2' and not info.get('backing-filename') and not info.get('encrypted')
        source_hash=run(['sudo','-n','sha256sum',source_path],timeout=300).stdout.split()[0]
        report['sourceSHA256']=source_hash
        target='/var/lib/libvirt/images/virmill-reboot-'+vmid+'.qcow2'
        assert run(['sudo','-n','test','-e',target],check=False).returncode==1
        run(['sudo','-n','cp','--reflink=auto','--sparse=always','--no-clobber',source_path,target],timeout=300)
        assert run(['sudo','-n','sha256sum',target],timeout=300).stdout.split()[0]==source_hash
        run(['sudo','-n','chown','qemu:qemu',target]);run(['sudo','-n','chmod','0600',target]);run(['sudo','-n','restorecon',target])
        report['testDisk']=target
        original.find('uuid').text=vmid;original.find('name').text='virmill-reboot-'+vmid[:8]
        for node in original.findall('metadata'): original.remove(node)
        for tag in ('memory','currentMemory'): original.find(tag).text='2097152'
        original.find('vcpu').text='2'
        devices=original.find('devices')
        for tag in ('interface','hostdev','filesystem','channel'):
            for node in devices.findall(tag): devices.remove(node)
        disks[0].set('type','file');selected.attrib.clear();selected.set('file',target)
        xmlpath=result_dir/'domain.xml';xmlpath.write_bytes(ET.tostring(original))
        assert virsh('domstate',args.source_vm).strip()=='shut off'
        virsh('define',str(xmlpath));defined=True
        for variable,part in [('XDG_STATE_HOME','state'),('XDG_RUNTIME_DIR','runtime'),('XDG_CACHE_HOME','cache'),('XDG_DATA_HOME','data'),('XDG_CONFIG_HOME','config')]:
            p=root/part;p.mkdir(mode=0o700);env[variable]=str(p)
        with (result_dir/'daemon.log').open('w') as log:
            daemon=subprocess.Popen([str(root/'bin/virmilld')],stdout=log,stderr=log,env=env)
            for _ in range(100):
                if (root/'runtime/virmill/control.sock').exists(): break
                assert daemon.poll() is None
                time.sleep(.1)
            assert (root/'runtime/virmill/control.sock').exists()
            _,started=operation('start');assert started['error'] is None and started['data']['state']=='succeeded',started
            time.sleep(45)  # Only the new copied guest is booting; no readiness claim.
            plan,rebooted=operation('reboot');report['rebootPlan']=plan;report['rebootResult']=rebooted
            if rebooted['error'] is not None or rebooted['data']['state']!='succeeded':raise RuntimeError('native reboot did not complete; retained without retry')
            db=sqlite3.connect('file:'+str(root/'state/virmill/journal.db')+'?mode=ro',uri=True)
            receipt=db.execute('SELECT body FROM metadata WHERE kind=? AND id=?',('vm-reboot-receipt',rebooted['data']['operationID'])).fetchone();db.close()
            assert receipt;report['receipt']=json.loads(receipt[0])
            assert report['receipt']['observation']['resource']['resourceUUID']==vmid
            assert report['receipt']['observation']['evidence']=='libvirt-reboot-event'
            report['status']='passed'
    except BaseException as exc:
        report['error']=str(exc)
    finally:
        if defined:
            try:
                if virsh('domstate',vmid).strip()!='shut off':
                    virsh('shutdown',vmid)
                    for _ in range(60):
                        if virsh('domstate',vmid).strip()=='shut off':break
                        time.sleep(1)
                    else:
                        # This fixed UUID owns only the newly copied disposable disk.
                        virsh('destroy',vmid);report['cleanupForceStop']=True
                report['testVMStopped']=virsh('domstate',vmid).strip()=='shut off'
                if not report['testVMStopped']:report['status']='failed'
            except BaseException as exc:report['cleanupError']=str(exc);report['status']='failed'
        if daemon is not None:
            daemon.terminate()
            try:daemon.wait(timeout=10)
            except subprocess.TimeoutExpired:report['daemonStopError']='did not exit';report['status']='failed'
        if before is not None:
            try:
                report['priorDefinitionsPreserved']=inventory()==before
                if source_path and source_hash:report['sourcePreserved']=run(['sudo','-n','sha256sum',source_path],timeout=300).stdout.split()[0]==source_hash
                if not report.get('priorDefinitionsPreserved') or not report.get('sourcePreserved'):report['status']='failed'
            except BaseException as exc:report['preservationError']=str(exc);report['status']='failed'
        (result_dir/'report.json').write_text(json.dumps(report,indent=2)+'\n')
        print(json.dumps(report,sort_keys=True))
    raise SystemExit(report['status']!='passed')


if __name__=='__main__': main()
