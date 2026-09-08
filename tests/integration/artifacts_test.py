#!/usr/bin/env python3
"""Development package structure, staged preservation and private daemon integration."""
import json
import hashlib
import io
import os
from pathlib import Path
import subprocess
import tempfile
import tarfile
import time
import unittest

ROOT = Path(__file__).resolve().parents[2]

class Artifacts(unittest.TestCase):
    def test_staged_install_and_uninstall_preserve_user_artifacts(self):
        with tempfile.TemporaryDirectory(prefix='virmill-package-test-') as temp:
            root = Path(temp)
            preserve = root/'var/lib/libvirt/images/owner-disk.qcow2'
            preserve.parent.mkdir(parents=True)
            preserve.write_bytes(b'synthetic user-owned data; never touched')
            command = ['python3',str(ROOT/'packaging/install.py')]
            options = ['--stage',str(ROOT/'build/package-stage/virmill'),'--destdir',str(root)]
            subprocess.run(command+['plan']+options,check=True,capture_output=True)
            subprocess.run(command+['install']+options,check=True,capture_output=True)
            self.assertTrue((root/'usr/bin/virmill').is_file())
            subprocess.run(command+['uninstall']+options,check=True,capture_output=True)
            self.assertFalse((root/'usr/bin/virmill').exists())
            self.assertEqual(preserve.read_bytes(), b'synthetic user-owned data; never touched')

    def test_debian_archive_structure_and_rpm_metadata(self):
        for filename in ('virmill_1.0.0~beta.1_amd64.deb','virmill-host-helper_1.0.0~beta.1_amd64.deb'):
            names = subprocess.check_output(['ar','t',str(ROOT/'dist'/filename)],text=True).splitlines()
            self.assertEqual(names,['debian-binary','control.tar.xz','data.tar.xz'])
        for filename in ('virmill-1.0.0-0.beta.1.x86_64.rpm','virmill-host-helper-1.0.0-0.beta.1.x86_64.rpm'):
            result = subprocess.run(['rpm','-qp','--queryformat','%{NAME} %{VERSION} %{RELEASE}\n',str(ROOT/'dist'/filename)],check=True,capture_output=True,text=True)
            self.assertIn(' 1.0.0 0.beta.1\n',result.stdout)

    def test_private_daemon_cli_and_lab_validation(self):
        if os.environ.get('VIRMILL_TEST_REQUIRE_IPC') != '1':
            self.skipTest('explicit temporary Unix-socket service integration run required')
        with tempfile.TemporaryDirectory(prefix='virmill-service-test-') as temp:
            root = Path(temp)
            env = dict(os.environ)
            for variable,name in [('XDG_STATE_HOME','state'),('XDG_RUNTIME_DIR','runtime'),('XDG_CACHE_HOME','cache'),('XDG_DATA_HOME','data'),('XDG_CONFIG_HOME','config')]:
                p = root/name
                p.mkdir(mode=0o700)
                env[variable] = str(p)
            log = (root/'daemon.log').open('w+')
            daemon = subprocess.Popen([str(ROOT/'build/bin/virmilld')],env=env,stdout=log,stderr=log)
            try:
                socket = root/'runtime/virmill/control.sock'
                for _ in range(100):
                    if socket.exists():
                        break
                    if daemon.poll() is not None:
                        log.seek(0)
                        self.fail(log.read())
                    time.sleep(0.02)
                self.assertTrue(socket.exists())
                for args in (['doctor'],['lab','validate',str(ROOT/'examples/labs/multi-network-lab.yaml')],['plugin','validate',str(ROOT/'tests/fixtures/plugins/python-summary')],['operation','list']):
                    result = subprocess.run([str(ROOT/'build/bin/virmill')]+args+['--output','json','--non-interactive'],env=env,check=True,capture_output=True,text=True)
                    response = json.loads(result.stdout)
                    self.assertEqual(response['apiVersion'],'virmill/v1')
                    self.assertIsNone(response['error'])
                self.assertEqual(socket.stat().st_mode & 0o777,0o600)
                policy = str(ROOT/'virmill-v1-spec/examples/backup-policy.yaml')
                for action in ('validate', 'preview'):
                    args = [str(ROOT/'build/bin/virmill'),'backup','policy',action,policy,'--output','json','--non-interactive']
                    if action == 'preview':
                        args += ['--input','{"after":"2026-09-08T00:00:00Z","count":2}']
                    reply = json.loads(subprocess.check_output(args,env=env,text=True))
                    self.assertIsNone(reply['error'])
                    self.assertTrue(reply['data']['valid'])
                    self.assertFalse(reply['data']['scheduleInstalled'])
                    self.assertFalse(reply['data']['captureVerified'])
                    if action == 'preview':
                        self.assertEqual(reply['data']['nextRuns'], ['2026-09-08T23:00:00Z','2026-09-09T23:00:00Z'])
                self.check_tui_search(root, env)
                # CLI paths originate in this temporary client directory, while
                # the coordinator was started from the repository directory.
                command = [str(ROOT/'build/bin/virmill')]
                resource_input = json.loads((ROOT/'examples/configuration/fixed-resources.json').read_text())
                for forbidden in (False, True):
                    selected = dict(resource_input)
                    if forbidden:
                        selected['password'] = 'resource-secret-fixture-sentinel'
                    rejected = subprocess.run(command+['vm','set','30f4fc6d-ed33-411f-bb40-ff8b2128d660',
                        '--connection','test:///default','--input',json.dumps(selected),'--plan',
                        '--output','json','--non-interactive'],cwd=root,env=env,capture_output=True,text=True,timeout=30)
                    self.assertNotEqual(rejected.returncode,0)
                    self.assertNotIn('resource-secret-fixture-sentinel',rejected.stdout+rejected.stderr)
                    error = json.loads(rejected.stdout)['error']
                    self.assertEqual(error['code'],'INVALID_INPUT' if forbidden else 'UNSUPPORTED_CAPABILITY')
                print('Packaged CPU/RAM preview: strict resource schema and native test-URI refusal pass; no host configuration effect')
                preview = subprocess.run(command+['plugin','new','./generated',
                    '--id','example.virmill.integration','--sdk-directory',str(ROOT/'sdk/go'),
                    '--output','json','--non-interactive'],cwd=root,env=env,check=True,capture_output=True,text=True)
                plan = json.loads(preview.stdout)['data']
                self.assertEqual(plan['review']['destination'],str(root/'generated'))
                self.assertFalse((root/'generated').exists())
                applied = subprocess.run(command+['plan','apply',plan['planID'],
                    '--digest',plan['planDigest'],'--idempotency-key','scaffold-fixture',
                    '--ack','write-plugin-artifact','--wait','--output','json','--non-interactive'],
                    cwd=root,env=env,check=True,capture_output=True,text=True)
                self.assertEqual(json.loads(applied.stdout)['data']['state'],'succeeded')
                manifest = json.loads((root/'generated/manifest.json').read_text())
                self.assertEqual(manifest['id'],'example.virmill.integration')
                self.assertTrue((root/'generated/sdk/server.go').is_file())
                if os.environ.get('VIRMILL_TEST_DISK_TOOLS') == '1':
                    self.prepare_fixture_through_cli(root, env, command)
                    self.prepare_disk_fixture_through_cli(root, env, command)
                    self.prepare_installation_through_cli(root, env, command)
            finally:
                daemon.terminate()
                daemon.wait(timeout=5)
                log.close()

    def check_tui_search(self, root, env):
        import fcntl
        import pty
        import select
        import struct
        import termios
        master, slave = pty.openpty()
        fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',24,80,0,0))
        proc = subprocess.Popen([str(ROOT/'build/bin/virmill'),'tui'],stdin=slave,stdout=slave,stderr=slave,env=dict(env,TERM='xterm-256color'))
        os.close(slave)
        transcript = bytearray()
        def receive(expected):
            until = time.monotonic()+5
            start = len(transcript)
            while time.monotonic()<until:
                self.assertIsNone(proc.poll(), 'TUI exited before keyboard check')
                if select.select([master],[],[],.05)[0]:
                    transcript.extend(os.read(master,8192))
                self.assertLess(len(transcript),256<<10)
                if expected.encode() in transcript[start:]:
                    return
            self.fail('TUI keyboard observation missing: '+expected)
        try:
            receive('Overview')
            os.write(master,b':');receive('Actions - all tools')
            os.write(master,b'/');receive('Find action:')
            os.write(master,b'policy preview');receive('Find action: policy preview')
            os.write(master,b'\r');receive('Enter Select')
            os.write(master,b'/');receive('Enter Keep matches')
            os.write(master,b'no-such-command-xyz');receive('No matching actions.')
            os.write(master,b'\x1b');time.sleep(.05)
            os.write(master,b'\x1b');receive('Overview')
            os.write(master,b'q')
            proc.wait(timeout=5);self.assertEqual(proc.returncode,0)
            print('Actual 80x24 workspace PTY: Actions catalog, search, select-only Enter, clear, back and no-results feedback passed; no action was submitted')
        finally:
            if proc.poll() is None:
                proc.terminate();proc.wait(timeout=5)
            os.close(master)
            (root/'tui-search-transcript.log').write_bytes(transcript)

    def prepare_fixture_through_cli(self, root, env, command):
        # Generated content only: this tests the actual CLI, daemon, journal,
        # namespace and qemu-img paths, without defining or booting a guest.
        ovf = b'''<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1" xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData"><References><File ovf:id="f1" ovf:href="boot.raw"/><File ovf:id="f2" ovf:href="data.raw"/></References><DiskSection><Disk ovf:diskId="boot" ovf:fileRef="f1"/><Disk ovf:diskId="data" ovf:fileRef="f2"/></DiskSection><VirtualSystem ovf:id="cli-fixture"><VirtualHardwareSection><Item><rasd:ResourceType>17</rasd:ResourceType><rasd:HostResource>ovf:/disk/boot</rasd:HostResource></Item><Item><rasd:ResourceType>17</rasd:ResourceType><rasd:HostResource>ovf:/disk/data</rasd:HostResource></Item></VirtualHardwareSection></VirtualSystem></Envelope>'''
        archive = root/'fixture.ova'
        with tarfile.open(archive, 'w', format=tarfile.USTAR_FORMAT) as output:
            for name, data in [('fixture.ovf',ovf),('boot.raw',b'Virmill boot fixture'.ljust(1048576,b'\0')),('data.raw',b'Virmill data fixture'.ljust(1048576,b'\0'))]:
                member = tarfile.TarInfo(name)
                member.size, member.mode, member.mtime = len(data), 0o600, 0
                output.addfile(member,io.BytesIO(data))
        original = hashlib.sha256(archive.read_bytes()).hexdigest()
        def invoke(*args):
            result = subprocess.run(command+list(args)+['--output','json','--non-interactive'],cwd=root,env=env,check=True,capture_output=True,text=True,timeout=30)
            response = json.loads(result.stdout)
            self.assertIsNone(response['error'])
            return response['data']
        inputs = dict(destination='./prepared',systemID='cli-fixture',disks=[dict(id=name,format='raw',maximumVirtualBytes=1048576) for name in ('boot','data')])
        plan = invoke('import','prepare','./fixture.ova','--input',json.dumps(inputs),'--plan')
        self.assertEqual(plan['review']['destination'],str(root/'prepared'))
        self.assertFalse((root/'prepared').exists())
        job = invoke('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key','import-fixture','--ack','write-import-artifacts','--wait')
        self.assertEqual(job['state'],'succeeded')
        result = invoke('import','result',job['operationID'])
        artifact = invoke('import','verify','./prepared')
        self.assertEqual(artifact,result['artifact'])
        self.assertEqual([d['sourceID'] for d in artifact['disks']],['boot','data'])
        self.assertFalse(artifact['vmDefined'])
        self.assertFalse(artifact['guestBootVerified'])
        self.assertEqual(hashlib.sha256(archive.read_bytes()).hexdigest(),original)
        print('Native CLI/daemon OVA preparation: original SHA-256', original, 'output disk hashes', [d['sha256'] for d in artifact['disks']], '; no guest boot')

        # Exercise the real daemon's creation registration and approved-source
        # path, then deliberately stop at the production provider's test-URI
        # refusal. No call reaches a host libvirt daemon or mutates its storage.
        creation = json.loads((ROOT/'examples/creation/prepared-ova.json').read_text())
        creation['hardware']['nics'] = []  # This OVF has no original NICs.
        before_jobs = invoke('operation','list')
        rejected = subprocess.run(command+['vm','create',job['operationID'],
            '--connection','test:///default','--input',json.dumps(creation),
            '--plan','--output','json','--non-interactive'],cwd=root,env=env,
            capture_output=True,text=True,timeout=30)
        self.assertNotEqual(rejected.returncode,0)
        error = json.loads(rejected.stdout)['error']
        self.assertEqual(error['code'],'UNSUPPORTED_CAPABILITY')
        self.assertIn('only explicit local qemu:///system or qemu:///session',error['message'])
        self.assertEqual(invoke('operation','list'),before_jobs)
        self.assertEqual(invoke('import','verify','./prepared'),artifact)
        wrong_result = subprocess.run(command+['vm','creation','result',job['operationID'],
            '--output','json','--non-interactive'],cwd=root,env=env,
            capture_output=True,text=True,timeout=30)
        self.assertNotEqual(wrong_result.returncode,0)
        self.assertEqual(json.loads(wrong_result.stdout)['error']['code'],'INVALID_INPUT')
        cleanup = subprocess.run(command+['vm','creation','cleanup',job['operationID'],
            '--connection','qemu:///session','--input','{"disposition":"retain"}',
            '--plan','--output','json','--non-interactive'],cwd=root,env=env,
            capture_output=True,text=True,timeout=30)
        self.assertNotEqual(cleanup.returncode,0)
        self.assertEqual(json.loads(cleanup.stdout)['error']['code'],'PERMISSION_DENIED')
        self.assertEqual(invoke('operation','list'),before_jobs)
        # The completed import uses another connection and is not a creation;
        # authority validation must reject it before any host backend access.
        print('Creation CLI/daemon source validation reached native test-URI refusal; no creation job, native storage effect or guest boot occurred')

    def prepare_disk_fixture_through_cli(self, root, env, command):
        source = root/'selected-disks'
        (source/'disks').mkdir(parents=True,mode=0o700)
        (source/'base.raw').write_bytes(b'Virmill selected base fixture'.ljust(1048576,b'\0'))
        (source/'data.raw').write_bytes(b'Virmill selected data fixture'.ljust(1048576,b'\0'))
        subprocess.run(['/usr/bin/qemu-img','create','-f','qcow2','-F','raw','-b','../base.raw',
                        str(source/'disks/boot.qcow2'),'1M'],check=True,capture_output=True)
        names = ['base.raw','data.raw','disks/boot.qcow2']
        originals = {name:hashlib.sha256((source/name).read_bytes()).hexdigest() for name in names}
        def invoke(*args):
            result = subprocess.run(command+list(args)+['--output','json','--non-interactive'],cwd=root,env=env,check=True,capture_output=True,text=True,timeout=30)
            response = json.loads(result.stdout)
            self.assertIsNone(response['error'])
            return response['data']
        inputs = dict(destination='./prepared-disks',offlineSources=True,
                      files=[dict(path=name,sha256=originals[name]) for name in names],
                      disks=[dict(id='boot',path='disks/boot.qcow2',format='qcow2',maximumVirtualBytes=1048576),
                             dict(id='data',path='data.raw',format='raw',maximumVirtualBytes=1048576)])
        plan = invoke('import','prepare-disks','./selected-disks','--input',json.dumps(inputs),'--plan')
        self.assertFalse((root/'prepared-disks').exists())
        self.assertEqual(plan['review']['sourceDirectory'],str(source))
        self.assertEqual(plan['review']['sourceLockProtocol'],'qemu-file-ofd-permissions-v1')
        job = invoke('plan','apply',plan['planID'],'--digest',plan['planDigest'],
                     '--idempotency-key','selected-disk-fixture','--ack','write-import-artifacts',
                     '--ack','offline-source-files','--wait')
        self.assertEqual(job['state'],'succeeded')
        result = invoke('import','result',job['operationID'])
        artifact = invoke('import','verify','./prepared-disks')
        self.assertEqual(artifact,result['artifact'])
        self.assertEqual(artifact['kind'],'PreparedDiskSet')
        self.assertEqual([d['sourceID'] for d in artifact['disks']],['boot','data'])
        self.assertEqual(artifact['system']['hardware'],[])
        self.assertFalse(artifact['vmDefined'])
        self.assertFalse(artifact['guestBootVerified'])
        self.assertEqual({name:hashlib.sha256((source/name).read_bytes()).hexdigest() for name in names},originals)
        creation = json.loads((ROOT/'examples/creation/prepared-disks.json').read_text())
        before = invoke('operation','list')
        rejected = subprocess.run(command+['vm','create',job['operationID'],'--connection','test:///default',
                                  '--input',json.dumps(creation),'--plan','--output','json','--non-interactive'],
                                  cwd=root,env=env,capture_output=True,text=True,timeout=30)
        self.assertNotEqual(rejected.returncode,0)
        error = json.loads(rejected.stdout)['error']
        self.assertEqual(error['code'],'UNSUPPORTED_CAPABILITY')
        self.assertIn('only explicit local qemu:///system or qemu:///session',error['message'])
        self.assertEqual(invoke('operation','list'),before)
        cloud = json.loads((ROOT/'examples/creation/nocloud.json').read_text())
        cloud['provisioning']['sourceSHA256'] = originals['disks/boot.qcow2']
        cloud_rejected = subprocess.run(command+['vm','create',job['operationID'],'--connection','test:///default',
                                       '--input',json.dumps(cloud),'--plan','--output','json','--non-interactive'],
                                       cwd=root,env=env,capture_output=True,text=True,timeout=30)
        self.assertNotEqual(cloud_rejected.returncode,0)
        cloud_error = json.loads(cloud_rejected.stdout)['error']
        self.assertEqual(cloud_error['code'],'UNSUPPORTED_CAPABILITY')
        self.assertIn('only explicit local qemu:///system or qemu:///session',cloud_error['message'])
        self.assertEqual(invoke('operation','list'),before)
        self.assertFalse(any((root/'cache/virmill').glob('seed-preview-*')))
        print('Native CLI/daemon NoCloud preview: actual identity-bound seed built/read back in confinement and preview files cleaned; declared generated source is nonbootable; native test URI refused without a host effect')
        print('Native CLI/daemon selected-file preparation: source hashes',originals,
              'output disk hashes',[d['sha256'] for d in artifact['disks']],
              '; private receipt accepted by creation; native test-URI refused; no host storage effect or VM boot')

    def prepare_installation_through_cli(self, root, env, command):
        source_dir = root/'iso-source'
        source_dir.mkdir(mode=0o700)
        (source_dir/'README.txt').write_text('Virmill generated nonbootable installation fixture\n')
        source = root/'installer.iso'
        subprocess.run(['/usr/bin/genisoimage','-quiet','-V','VIRMILL_TEST','-o',str(source),str(source_dir)],check=True,capture_output=True)
        original = hashlib.sha256(source.read_bytes()).hexdigest()
        def invoke(*args):
            result = subprocess.run(command+list(args)+['--output','json','--non-interactive'],cwd=root,env=env,check=True,capture_output=True,text=True,timeout=30)
            response = json.loads(result.stdout)
            self.assertIsNone(response['error'])
            return response['data']
        inputs = dict(destination='./prepared-installation',offlineSources=True,sha256=original,mediaID='installer',disks=[dict(id='boot',virtualBytes=1048576),dict(id='data',virtualBytes=2097152)])
        plan = invoke('import','prepare-install','./installer.iso','--input',json.dumps(inputs),'--plan')
        self.assertEqual(plan['review']['destination'],str(root/'prepared-installation'))
        self.assertFalse((root/'prepared-installation').exists())
        job = invoke('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key','installation-fixture','--ack','write-import-artifacts','--ack','offline-source-files','--wait')
        self.assertEqual(job['state'],'succeeded')
        artifact = invoke('import','verify','./prepared-installation')
        self.assertEqual(artifact,invoke('import','result',job['operationID'])['artifact'])
        self.assertEqual(artifact['kind'],'PreparedInstallation')
        self.assertEqual(artifact['media'][0]['sha256'],original)
        self.assertEqual([d['virtualBytes'] for d in artifact['disks']],[1048576,2097152])
        self.assertTrue(all(d['verification']=='virtual-size+qemu-check+zero-map' and d['sourceChain']==[] for d in artifact['disks']))
        self.assertFalse(artifact['vmDefined'])
        self.assertFalse(artifact['guestBootVerified'])
        self.assertEqual(hashlib.sha256(source.read_bytes()).hexdigest(),original)
        creation = json.loads((ROOT/'examples/creation/prepared-installation.json').read_text())
        before = invoke('operation','list')
        rejected = subprocess.run(command+['vm','create',job['operationID'],'--connection','test:///default','--input',json.dumps(creation),'--plan','--output','json','--non-interactive'],cwd=root,env=env,capture_output=True,text=True,timeout=30)
        self.assertNotEqual(rejected.returncode,0)
        error = json.loads(rejected.stdout)['error']
        self.assertEqual(error['code'],'UNSUPPORTED_CAPABILITY')
        self.assertIn('only explicit local qemu:///system or qemu:///session',error['message'])
        self.assertEqual(invoke('operation','list'),before)
        print('Native CLI/daemon ISO preparation: copied media SHA-256',original,'empty disk hashes',[d['sha256'] for d in artifact['disks']],'; approved source reaches native test-URI refusal; no host storage mutation or guest boot')

if __name__ == '__main__':
    unittest.main(verbosity=2)
