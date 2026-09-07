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
        for filename in ('virmill_0.0.0~dev_amd64.deb','virmill-host-helper_0.0.0~dev_amd64.deb'):
            names = subprocess.check_output(['ar','t',str(ROOT/'dist'/filename)],text=True).splitlines()
            self.assertEqual(names,['debian-binary','control.tar.xz','data.tar.xz'])
        for filename in ('virmill-0.0.0-0.dev.x86_64.rpm','virmill-host-helper-0.0.0-0.dev.x86_64.rpm'):
            result = subprocess.run(['rpm','-qp','--queryformat','%{NAME} %{VERSION}\n',str(ROOT/'dist'/filename)],check=True,capture_output=True,text=True)
            self.assertIn('0.0.0',result.stdout)

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
                # CLI paths originate in this temporary client directory, while
                # the coordinator was started from the repository directory.
                command = [str(ROOT/'build/bin/virmill')]
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
            finally:
                daemon.terminate()
                daemon.wait(timeout=5)
                log.close()

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

if __name__ == '__main__':
    unittest.main(verbosity=2)
