#!/usr/bin/env python3
"""Build unsigned development RPM/DEB artifacts without installing or publishing."""
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[1]
DIST = ROOT/'dist'
DIST.mkdir(exist_ok=True)
EPOCH = int(os.environ.get('SOURCE_DATE_EPOCH', '0'))


def tar_bytes(files):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w:xz', format=tarfile.USTAR_FORMAT) as archive:
        for name, (content, mode) in sorted(files.items()):
            h = tarfile.TarInfo(name)
            h.size, h.mode, h.mtime = len(content), mode, EPOCH
            archive.addfile(h, io.BytesIO(content))
    return output.getvalue()


def ar(path, members):
    with path.open('wb') as out:
        out.write(b'!<arch>\n')
        for name, content in members:
            h = f'{name+"/":<16}{EPOCH:<12}{0:<6}{0:<6}{"100644":<8}{len(content):<10}`\n'
            assert len(h) == 60
            out.write(h.encode('ascii'))
            out.write(content)
            if len(content) % 2:
                out.write(b'\n')


def content(path, mode=0o644):
    return (ROOT/path).read_bytes(), mode

core = {
    'usr/bin/virmill': content('build/bin/virmill',0o755),
    'usr/bin/virmilld': content('build/bin/virmilld',0o755),
    'usr/lib/systemd/user/virmilld.service': content('packaging/systemd/virmilld.service'),
    'usr/share/licenses/virmill/LICENSE': content('LICENSE'),
    'usr/share/bash-completion/completions/virmill': content('packaging/completions/virmill.bash'),
    'usr/share/zsh/site-functions/_virmill': content('packaging/completions/virmill.zsh'),
    'usr/share/fish/vendor_completions.d/virmill.fish': content('packaging/completions/virmill.fish'),
}
for tree, target in [('docs','usr/share/doc/virmill'),('schemas','usr/share/virmill/schemas'),('sdk/go','usr/share/virmill/sdk/go'),('examples','usr/share/virmill/examples')]:
    for p in sorted((ROOT/tree).rglob('*')):
        if p.is_file() and p.suffix in ('.md','.json','.go','.yaml','.py','.mod') and 'evidence/logs' not in str(p):
            core[target+'/'+p.relative_to(ROOT/tree).as_posix()] = p.read_bytes(), 0o644
helper = {
    'usr/libexec/virmill-host-helper': content('build/bin/virmill-host-helper',0o755),
    'usr/lib/systemd/system/virmill-host-helper.service': content('packaging/systemd/virmill-host-helper.service'),
    'usr/lib/systemd/system/virmill-host-helper.socket': content('packaging/systemd/virmill-host-helper.socket'),
    'usr/share/doc/virmill-host-helper/helper-policy.example.json': content('packaging/policy/helper-policy.example.json'),
    'usr/share/licenses/virmill-host-helper/LICENSE': content('LICENSE'),
}
for name, files, dependencies in [('virmill',core,'libc6, libvirt0, qemu-system-x86, qemu-utils'),('virmill-host-helper',helper,'libc6')]:
    description = 'Virmill development build; incomplete and not release-qualified'
    control = f'Package: {name}\nVersion: 0.0.0~dev\nArchitecture: amd64\nMaintainer: Virmill contributors\nSection: admin\nPriority: optional\nDepends: {dependencies}\nDescription: {description}\n'
    ar(DIST/f'{name}_0.0.0~dev_amd64.deb', [('debian-binary',b'2.0\n'),('control.tar.xz',tar_bytes({'control':(control.encode(),0o644)})),('data.tar.xz',tar_bytes(files))])
    stage = ROOT/'build/package-stage'/name
    if stage.exists():
        shutil.rmtree(stage)  # owned, regenerated build staging only
    stage.mkdir(parents=True)
    for path,(data,mode) in files.items():
        dest = stage/path
        dest.parent.mkdir(parents=True,exist_ok=True)
        dest.write_bytes(data)
        dest.chmod(mode)
    rpm = ROOT/'build/rpm'/name
    for folder in ('BUILD','BUILDROOT','SPECS','SOURCES','RPMS','SRPMS'):
        (rpm/folder).mkdir(parents=True,exist_ok=True)
    filelist = '\n'.join('/'+p for p in sorted(files))
    requirements = 'Requires: libvirt-libs, qemu-kvm, qemu-img\n' if name == 'virmill' else ''
    spec = f'''Name: {name}
Version: 0.0.0
Release: 0.dev
Summary: {description}
License: MIT
BuildArch: x86_64
{requirements}
%description
{description}. No service, host network or privilege setup is enabled on install.

%install
mkdir -p %{{buildroot}}
cp -a {stage}/. %{{buildroot}}/

%files
{filelist}
'''
    specpath = rpm/'SPECS'/f'{name}.spec'
    specpath.write_text(spec)
    result = subprocess.run(['rpmbuild','-bb','--define',f'_topdir {rpm}','--define',f'_tmppath {ROOT / "build"}', '--define','_build_id_links none','--define','_buildhost virmill.local','--define','source_date_epoch_from_changelog 0','--define','use_source_date_epoch_as_buildtime 1','--define','clamp_mtime_to_source_date_epoch 1','--define','__os_install_post %{nil}',str(specpath)],capture_output=True,text=True,env={**os.environ,'SOURCE_DATE_EPOCH':str(EPOCH)})
    (ROOT/'build'/f'{name}-rpmbuild.log').write_text(result.stdout+result.stderr)
    if result.returncode:
        raise SystemExit(result.stderr)
    for p in (rpm/'RPMS').rglob('*.rpm'):
        shutil.copy2(p,DIST/p.name)
    manifest = {'product':'Virmill','package':name,'releaseQualified':False,'files':[{'path':path,'sha256':hashlib.sha256(data).hexdigest(),'mode':mode} for path,(data,mode) in sorted(files.items())]}
    (stage/'install-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
artifacts = [{'path':p.name,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()} for p in sorted(DIST.iterdir()) if p.suffix in ('.rpm','.deb')]
(DIST/'checksums.json').write_text(json.dumps({'releaseQualified':False,'signed':False,'artifacts':artifacts},indent=2)+'\n')
print(json.dumps({'built':artifacts,'releaseQualified':False,'installed':False,'published':False},indent=2))
