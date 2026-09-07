#!/usr/bin/env python3
"""Build unsigned development RPM/DEB artifacts from a Git checkout only."""
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import shlex
import shutil
import stat
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[1]
PACKAGE_NAMES = ('virmill', 'virmill-host-helper')
BINARY_PATHS = tuple('build/bin/' + name for name in ('virmill', 'virmilld', 'virmill-host-helper'))
PACKAGE_ARTIFACTS = tuple(sorted(
    filename for name in PACKAGE_NAMES for filename in
    (f'{name}_0.0.0~dev_amd64.deb', f'{name}-0.0.0-0.dev.x86_64.rpm')))
SOURCE_TREES = (('docs', 'usr/share/doc/virmill'),
                ('schemas', 'usr/share/virmill/schemas'),
                ('sdk/go', 'usr/share/virmill/sdk/go'),
                ('examples', 'usr/share/virmill/examples'))
SOURCE_SUFFIXES = ('.md', '.json', '.go', '.yaml', '.py', '.mod')


def relative_parts(path):
    """Only accept literal, canonical paths beneath the checkout."""
    parsed = PurePosixPath(path)
    if not path or not parsed.parts or parsed.is_absolute() or parsed.as_posix() != path or '..' in parsed.parts:
        raise ValueError(f'Noncanonical package path: {path!r}')
    return parsed.parts


def rpm_literal_path(path):
    # RPM expands macros before parsing either shell quotes or %files quotes.
    # Refuse macro/control characters instead of treating shell quoting as enough.
    if '%' in path or any(ord(char) < 32 or ord(char) == 127 for char in path):
        raise ValueError(f'Package path contains RPM macro/control characters: {path!r}')
    return path


def rpm_file_path(path):
    path = rpm_literal_path(path)
    return '"' + path.replace('\\', '\\\\').replace('"', '\\"') + '"'


def rpm_install_lines(stage):
    source = shlex.quote(rpm_literal_path(str(stage)) + '/.')
    return f'mkdir -p "$RPM_BUILD_ROOT"\ncp -a -- {source} "$RPM_BUILD_ROOT/"'


def directory_fd(path, create=False):
    """Pin each directory component without following symbolic links."""
    path = Path(path).absolute()
    fd = os.open(path.anchor, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        for part in path.parts[1:]:
            if create:
                try:
                    os.mkdir(part, dir_fd=fd)
                except FileExistsError:
                    pass
            next_fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=fd)
            os.close(fd)
            fd = next_fd
        return fd
    except BaseException:
        os.close(fd)
        raise


def read_regular(root, relative):
    parts = relative_parts(relative)
    parent = directory_fd(Path(root).joinpath(*parts[:-1]))
    try:
        if not stat.S_ISREG(os.stat(parts[-1], dir_fd=parent, follow_symlinks=False).st_mode):
            raise ValueError(f'Package input is not a regular file: {relative}')
        # NONBLOCK prevents an unexpected FIFO from hanging before the type check.
        fd = os.open(parts[-1], os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=parent)
        with os.fdopen(fd, 'rb') as source:
            if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
                raise ValueError(f'Package input is not a regular file: {relative}')
            return source.read()
    finally:
        os.close(parent)


def write_regular(root, relative, data, mode=0o644):
    parts = relative_parts(relative)
    parent = directory_fd(Path(root).joinpath(*parts[:-1]), create=True)
    try:
        fd = os.open(parts[-1], os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC,
                     mode, dir_fd=parent)
        with os.fdopen(fd, 'wb') as output:
            info = os.fstat(output.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
                raise ValueError(f'Package output is not an ordinary single-link file: {relative}')
            os.ftruncate(output.fileno(), 0)
            os.fchmod(output.fileno(), mode)
            output.write(data)
    finally:
        os.close(parent)


def tracked_inputs(root):
    """Require this exact Git top level; linked worktrees are supported."""
    root = Path(root).absolute()
    os.close(directory_fd(root))
    top = subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], cwd=root, stderr=subprocess.PIPE)
    if Path(os.fsdecode(top).rstrip('\n')).absolute() != root:
        raise ValueError('Packaging requires the exact Git checkout root, not a directory in a parent repository')
    records = subprocess.check_output(['git', 'ls-files', '--stage', '-z'], cwd=root)
    tracked = {}
    for record in records.split(b'\0'):
        if not record:
            continue
        metadata, raw_path = record.split(b'\t', 1)
        mode, _, stage = metadata.split()
        path = os.fsdecode(raw_path)
        relative_parts(path)
        if stage != b'0' or path in tracked:
            raise ValueError(f'Unmerged or duplicate Git index entry: {path}')
        tracked[path] = mode
    return tracked


def collect_package_files(root):
    rpm_literal_path(str(Path(root).absolute()))
    tracked = tracked_inputs(root)

    def content(path, mode=0o644):
        rpm_literal_path(path)
        if path not in BINARY_PATHS and tracked.get(path) not in (b'100644', b'100755'):
            raise ValueError(f'Package source is not an indexed regular file: {path}')
        return read_regular(root, path), mode

    core = {
        'usr/bin/virmill': content('build/bin/virmill', 0o755),
        'usr/bin/virmilld': content('build/bin/virmilld', 0o755),
        'usr/lib/systemd/user/virmilld.service': content('packaging/systemd/virmilld.service'),
        'usr/share/licenses/virmill/LICENSE': content('LICENSE'),
        'usr/share/bash-completion/completions/virmill': content('packaging/completions/virmill.bash'),
        'usr/share/zsh/site-functions/_virmill': content('packaging/completions/virmill.zsh'),
        'usr/share/fish/vendor_completions.d/virmill.fish': content('packaging/completions/virmill.fish'),
    }
    for path in sorted(tracked):
        for tree, target in SOURCE_TREES:
            if path.startswith(tree + '/') and PurePosixPath(path).suffix in SOURCE_SUFFIXES and 'evidence/logs' not in path:
                core[target + '/' + path[len(tree) + 1:]] = content(path)
    helper = {
        'usr/libexec/virmill-host-helper': content('build/bin/virmill-host-helper', 0o755),
        'usr/lib/systemd/system/virmill-host-helper.service': content('packaging/systemd/virmill-host-helper.service'),
        'usr/lib/systemd/system/virmill-host-helper.socket': content('packaging/systemd/virmill-host-helper.socket'),
        'usr/share/doc/virmill-host-helper/helper-policy.example.json': content('packaging/policy/helper-policy.example.json'),
        'usr/share/licenses/virmill-host-helper/LICENSE': content('LICENSE'),
    }
    return {'virmill': core, 'virmill-host-helper': helper}


def reset_owned_directory(root, relative):
    owned = {f'build/{tree}/{name}' for tree in ('package-stage', 'rpm') for name in PACKAGE_NAMES}
    if relative not in owned:
        raise ValueError(f'Refusing to recreate an unowned directory: {relative}')
    path = Path(root) / relative
    parent = directory_fd(path.parent, create=True)
    try:
        try:
            info = os.stat(path.name, dir_fd=parent, follow_symlinks=False)
        except FileNotFoundError:
            pass
        else:
            if not stat.S_ISDIR(info.st_mode):
                raise ValueError(f'Package staging/output root is not an ordinary directory: {relative}')
            if not shutil.rmtree.avoids_symlink_attacks:
                raise ValueError('Packaging requires symlink-safe directory removal')
            shutil.rmtree(path.name, dir_fd=parent)
        os.mkdir(path.name, dir_fd=parent)
    finally:
        os.close(parent)
    return path


def tar_bytes(files, epoch):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w:xz', format=tarfile.USTAR_FORMAT) as archive:
        for name, (data, mode) in sorted(files.items()):
            entry = tarfile.TarInfo(name)
            entry.size, entry.mode, entry.mtime = len(data), mode, epoch
            archive.addfile(entry, io.BytesIO(data))
    return output.getvalue()


def ar_bytes(members, epoch):
    output = io.BytesIO()
    output.write(b'!<arch>\n')
    for name, data in members:
        header = f'{name+"/":<16}{epoch:<12}{0:<6}{0:<6}{"100644":<8}{len(data):<10}`\n'
        assert len(header) == 60
        output.write(header.encode('ascii'))
        output.write(data)
        if len(data) % 2:
            output.write(b'\n')
    return output.getvalue()


def copy_expected_rpm(root, name):
    if name not in PACKAGE_NAMES:
        raise ValueError(f'Unknown package: {name}')
    filename = f'{name}-0.0.0-0.dev.x86_64.rpm'
    data = read_regular(root, f'build/rpm/{name}/RPMS/x86_64/{filename}')
    write_regular(root, 'dist/' + filename, data)


def package_artifacts(root):
    return [{'path': filename, 'sha256': hashlib.sha256(read_regular(root, 'dist/' + filename)).hexdigest()}
            for filename in PACKAGE_ARTIFACTS]


def build_packages(root=ROOT):
    root = Path(root).absolute()
    epoch = int(os.environ.get('SOURCE_DATE_EPOCH', '0'))
    packages = collect_package_files(root)
    os.close(directory_fd(root / 'dist', create=True))
    for name, files in packages.items():
        stage = reset_owned_directory(root, f'build/package-stage/{name}')
        rpm = reset_owned_directory(root, f'build/rpm/{name}')
        dependencies = 'libc6, libvirt0, qemu-system-x86, qemu-utils, bubblewrap, util-linux, xorriso' if name == 'virmill' else 'libc6, libvirt0'
        description = 'Virmill development build; incomplete and not release-qualified'
        control = f'Package: {name}\nVersion: 0.0.0~dev\nArchitecture: amd64\nMaintainer: Virmill contributors\nSection: admin\nPriority: optional\nDepends: {dependencies}\nDescription: {description}\n'
        deb = ar_bytes([('debian-binary', b'2.0\n'),
                        ('control.tar.xz', tar_bytes({'control': (control.encode(), 0o644)}, epoch)),
                        ('data.tar.xz', tar_bytes(files, epoch))], epoch)
        write_regular(root, f'dist/{name}_0.0.0~dev_amd64.deb', deb)
        for path, (data, mode) in files.items():
            write_regular(root, f'build/package-stage/{name}/' + path, data, mode)
        for folder in ('BUILD', 'BUILDROOT', 'SPECS', 'SOURCES', 'RPMS', 'SRPMS'):
            os.close(directory_fd(rpm / folder, create=True))
        filelist = '\n'.join(rpm_file_path('/' + path) for path in sorted(files))
        requirements = 'Requires: libvirt-libs, qemu-kvm, qemu-img, bubblewrap, util-linux, xorriso\n' if name == 'virmill' else 'Requires: libvirt-libs\n'
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
{rpm_install_lines(stage)}

%files
{filelist}
'''
        specpath = rpm / 'SPECS' / f'{name}.spec'
        write_regular(root, f'build/rpm/{name}/SPECS/{name}.spec', spec.encode())
        result = subprocess.run(['rpmbuild', '-bb', '--define', f'_topdir {rpm}', '--define', f'_tmppath {root / "build"}', '--define', '_build_id_links none', '--define', '_buildhost virmill.local', '--define', 'source_date_epoch_from_changelog 0', '--define', 'use_source_date_epoch_as_buildtime 1', '--define', 'clamp_mtime_to_source_date_epoch 1', '--define', '__os_install_post %{nil}', str(specpath)], capture_output=True, text=True, env={**os.environ, 'SOURCE_DATE_EPOCH': str(epoch)})
        write_regular(root, f'build/{name}-rpmbuild.log', (result.stdout + result.stderr).encode())
        if result.returncode:
            raise RuntimeError(result.stderr)
        copy_expected_rpm(root, name)
        manifest = {'product': 'Virmill', 'package': name, 'releaseQualified': False, 'files': [
            {'path': path, 'sha256': hashlib.sha256(data).hexdigest(), 'mode': mode}
            for path, (data, mode) in sorted(files.items())]}
        write_regular(root, f'build/package-stage/{name}/install-manifest.json', (json.dumps(manifest, indent=2) + '\n').encode())
    artifacts = package_artifacts(root)
    write_regular(root, 'dist/checksums.json', (json.dumps({'releaseQualified': False, 'signed': False, 'artifacts': artifacts}, indent=2) + '\n').encode())
    return {'built': artifacts, 'releaseQualified': False, 'installed': False, 'published': False}


if __name__ == '__main__':
    print(json.dumps(build_packages(), indent=2))
