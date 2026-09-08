#!/usr/bin/env python3
"""Authorized disposable VM only: retain source guests/media and a new restore.

Source ACLs are temporarily granted to the ordinary test actor and restored
exactly. This is fixture setup, not proof of Virmill's separate grant workflow.
Only the newly restored guest is started/stopped. No source VM is redefined.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import uuid
import xml.etree.ElementTree as ET


def main():
    p=argparse.ArgumentParser()
    p.add_argument('--execute-disposable', action='store_true', required=True)
    p.add_argument('--root', type=Path, required=True)
    p.add_argument('--source-vm', required=True)
    p.add_argument('--source-root', type=Path, required=True)
    p.add_argument('--pool', required=True)
    args=p.parse_args()
    assert socket.gethostname()=='virmill-test' and os.getuid()==1000
    root=args.root.resolve(strict=True)
    assert root.is_relative_to(Path.home()/'virmill-tests')
    assert str(uuid.UUID(args.source_vm))==args.source_vm and str(uuid.UUID(args.pool))==args.pool
    source_root=str(args.source_root)
    assert source_root.startswith('/var/lib/libvirt/images/') and args.source_root.is_absolute()
    results=root/'results';results.mkdir(mode=0o700)
    env=dict(os.environ)
    events=[];report={'status':'failed','sourceVM':args.source_vm,'scope':'stopped two-disk BIOS capture and new disconnected restore; source bytes and definitions preserved; no auxiliary or encrypted repository qualification'}
    daemon=None;new_id=None;baseline=None;source_hashes={};acls={}
    def run(argv, timeout=120, check=True):
        start=time.monotonic()
        r=subprocess.run(argv,capture_output=True,text=True,timeout=timeout,env=env)
        events.append({'argv':argv,'exitCode':r.returncode,'seconds':round(time.monotonic()-start,3),'stdout':r.stdout,'stderr':r.stderr})
        (results/'commands.json').write_text(json.dumps(events,indent=2)+'\n')
        if check and r.returncode:raise RuntimeError('command failed: '+repr(argv))
        return r
    def native(*args, **kwargs):return run(['virsh','-c','qemu:///system',*args],**kwargs).stdout
    def digest(path):return run(['sudo','-n','sha256sum',str(path)],timeout=300).stdout.split()[0]
    def inventory():
        out={}
        for vmid in native('list','--all','--uuid').split():
            if vmid==new_id:continue
            assert native('domstate',vmid).strip()=='shut off'
            out[vmid]=hashlib.sha256(native('dumpxml',vmid,'--inactive').encode()).hexdigest()
        return out
    def cli(*argv,check=True):
        r=run([str(root/'bin/virmill'),*argv,'--output','json','--non-interactive'],timeout=600,check=check)
        value=json.loads(r.stdout)
        if check:assert value['error'] is None,value
        return value
    def apply(plan):
        argv=['plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key',str(uuid.uuid4()),'--wait','--timeout','10m']
        for ack in plan['acknowledgements']:argv+=['--ack',ack]
        result=cli(*argv,check=False)
        report.setdefault('operations',[]).append(result)
        assert result['error'] is None and result['data']['state']=='succeeded',result
        return result['data']
    try:
        report['binaries']=json.loads((root/'binaries.json').read_text())
        for name,wanted in report['binaries'].items():
            assert name in ('virmill','virmilld')
            assert hashlib.sha256((root/'bin'/name).read_bytes()).hexdigest()==wanted
        baseline=inventory();assert args.source_vm in baseline
        raw=native('dumpxml',args.source_vm,'--inactive');source_xml=ET.fromstring(raw)
        assert source_xml.find('./os/loader') is None and source_xml.find('./devices/tpm') is None
        disks=source_xml.findall('./devices/disk[@device="disk"]');assert len(disks)==2
        for i,disk in enumerate(disks):
            source=disk.find('source')
            if disk.get('type')=='volume':
                path=Path(native('vol-path','--pool',source.get('pool'),source.get('volume')).strip())
            else:path=Path(source.get('file'))
            assert path.parent==args.source_root and path.name.startswith('virmill-'+args.source_vm+'-')
            source_hashes[str(path)]=digest(path)
            acl=run(['sudo','-n','getfacl','--absolute-names',str(path)]).stdout
            aclfile=results/('source-'+str(i)+'.acl');aclfile.write_text(acl);acls[str(path)]=(acl,aclfile)
            run(['sudo','-n','setfacl','-m','u:1000:r--',str(path)])
        for variable,part in [('XDG_STATE_HOME','state'),('XDG_RUNTIME_DIR','runtime'),('XDG_CACHE_HOME','cache'),('XDG_DATA_HOME','data'),('XDG_CONFIG_HOME','config')]:
            directory=root/part;directory.mkdir(mode=0o700);env[variable]=str(directory)
        with (results/'daemon.log').open('w') as log:
            daemon=subprocess.Popen([str(root/'bin/virmilld')],env=env,stdout=log,stderr=log)
            for _ in range(100):
                if (root/'runtime/virmill/control.sock').exists():break
                assert daemon.poll() is None
                time.sleep(.1)
            assert (root/'runtime/virmill/control.sock').exists()
            plan=cli('snapshot','create',args.source_vm,'--input',json.dumps({'sourceRoot':source_root}),'--plan')['data']
            report['capturePlanID']=plan['planID'];report['snapshotID']=plan['review']['snapshotID']
            capture_job=apply(plan);report['captureOperationID']=capture_job['operationID']
            captured=cli('snapshot','show',report['snapshotID'])['data'];report['capture']=captured
            assert len(captured['manifest']['disks'])==2 and captured['manifest']['independentlyRecoverable']
            plan=cli('snapshot','restore',report['snapshotID'],'--input',json.dumps({'name':'Virmill cold restored BIOS '+report['snapshotID'][:8],'poolID':args.pool}),'--plan')['data']
            new_id=plan['review']['newVMID'];report['restoredVM']=new_id
            report['restorePlanID']=plan['planID'];restored=apply(plan);report['restoreOperationID']=restored['operationID']
            after=ET.fromstring(native('dumpxml',new_id,'--inactive'))
            assert after.find('uuid').text==new_id and after.find('name').text!=source_xml.find('name').text
            assert not after.findall('./devices/interface') and native('domstate',new_id).strip()=='shut off'
            restored_paths=[]
            for disk in after.findall('./devices/disk[@device="disk"]'):
                path=disk.find('source').get('file');assert path and path not in source_hashes
                info=json.loads(run(['sudo','-n','qemu-img','info','--output=json',path]).stdout)
                assert info['format']=='qcow2' and not info.get('backing-filename')
                restored_paths.append(path)
            assert len(restored_paths)==2;report['independentRestoredPaths']=restored_paths
            # Drop temporary original-disk read access before starting the clone.
            for path,(acl,aclfile) in acls.items():run(['sudo','-n','setfacl','--restore='+str(aclfile)])
            acls_restored=all(run(['sudo','-n','getfacl','--absolute-names',path]).stdout==acl for path,(acl,_) in acls.items())
            assert acls_restored;report['originalACLRestoredBeforeBoot']=True
            start_plan=cli('vm','start',new_id,'--plan')['data'];report['startOperationID']=apply(start_plan)['operationID']
            assert native('domstate',new_id).strip()=='running'
            readiness=cli('vm','readiness','show',new_id)['data']
            assert readiness['state']=='absent' and readiness['agentResponsive'] is False
            report['guestAgentObservation']=readiness
            time.sleep(4)
            native('screenshot',new_id,str(results/'restored-screen.ppm'))
            report['nativeGuestRunning']=True
            report['screenshot']='results/restored-screen.ppm'
            report['bootMarkerVerified']=False  # A separate visual read must verify the actual marker.
            report['status']='passed'
    except BaseException as exc:report['error']=repr(exc)
    finally:
        if new_id:
            try:
                state=run(['virsh','-c','qemu:///system','domstate',new_id],check=False)
                if state.returncode==0 and state.stdout.strip()!='shut off':
                    # This synthetic BIOS fixture has no guest OS shutdown agent.
                    # Only the newly restored UUID and its new disks are affected.
                    native('destroy',new_id);report['newFixtureStoppedByAuthorizedCleanup']=True
            except BaseException as exc:report['cleanupError']=repr(exc);report['status']='failed'
        if daemon is not None:
            daemon.terminate()
            try:daemon.wait(timeout=15)
            except subprocess.TimeoutExpired:report['daemonStopError']=True;report['status']='failed'
        for path,(acl,aclfile) in acls.items():
            try:
                run(['sudo','-n','setfacl','--restore='+str(aclfile)])
                assert run(['sudo','-n','getfacl','--absolute-names',path]).stdout==acl
            except BaseException as exc:report['aclRestoreError']=repr(exc);report['status']='failed'
        try:
            report['sourceDiskBytesPreserved']=all(digest(path)==wanted for path,wanted in source_hashes.items())
            report['sourceHashes']=source_hashes
            report['priorDefinitionsPreserved']=baseline is not None and inventory()==baseline
            if not report['sourceDiskBytesPreserved'] or not report['priorDefinitionsPreserved']:report['status']='failed'
        except BaseException as exc:report['preservationError']=repr(exc);report['status']='failed'
        (results/'report.json').write_text(json.dumps(report,indent=2)+'\n')
        print(json.dumps(report,sort_keys=True))
    raise SystemExit(report['status']!='passed')

if __name__=='__main__':main()
