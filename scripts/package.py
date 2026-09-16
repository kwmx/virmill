#!/usr/bin/env python3
"""Build unsigned development RPM/DEB artifacts from a Git checkout only."""
import gzip
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import shutil
import stat
import subprocess
import tarfile
import time

from package_docs import rewrite_markdown

ROOT = Path(__file__).resolve().parents[1]
PACKAGE_NAMES = ('virmill', 'virmill-host-helper')
BINARY_PATHS = tuple('build/bin/' + name for name in ('virmill', 'virmilld', 'virmill-host-helper'))
VERSION_SOURCE = 'internal/buildinfo/version.go'
SOURCE_TREES = (('docs', 'usr/share/doc/virmill'),
                ('schemas', 'usr/share/virmill/schemas'),
                ('sdk/go', 'usr/share/virmill/sdk/go'),
                ('examples', 'usr/share/virmill/examples'))
SOURCE_SUFFIXES = ('.md', '.json', '.go', '.yaml', '.py', '.mod')
PROTOCOL_SOURCE = 'virmill-v1-spec/docs/12-plugin-protocol.md'
PROTOCOL_TARGET = 'usr/share/doc/virmill/virmill-v1-spec/docs/12-plugin-protocol.md'
MAINTAINER = 'Faisal Alhisan <faisal@alhisan.com>'
HOMEPAGE = 'https://github.com/kwmx/virmill'
# util-linux is Essential on Debian and must not be listed.
DEB_DEPENDS = {'virmill': 'libc6, libvirt0, qemu-system-x86, qemu-utils, bubblewrap, xorriso',
               'virmill-host-helper': 'libc6, libvirt0'}
DEB_SUMMARY = {'virmill': 'local QEMU/KVM virtual machine manager with CLI and TUI',
               'virmill-host-helper': 'bounded privileged helper for the Virmill coordinator'}


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


def parse_product_version(data):
    """Read one literal beta constant; never accept an environment-selected version."""
    if len(data) > 16384:
        raise ValueError('Product version source exceeds 16 KiB')
    text = data.decode('utf-8', errors='strict')
    declarations = re.findall(r'(?m)^(?:const|var)\s+Version\b[^\n]*$', text)
    if len(declarations) != 1:
        raise ValueError('Product version requires exactly one literal const Version declaration')
    match = re.fullmatch(r'const Version = "([^"]+)"', declarations[0])
    if not match:
        raise ValueError('Product version requires a literal const Version declaration')
    version = match.group(1)
    beta_version_parts(version)
    return version


def beta_version_parts(version):
    # Restrict packaging to the explicitly implemented beta naming policy.
    # A final release or a different prerelease family needs a reviewed change.
    number = r'(?:0|[1-9][0-9]{0,8})'
    match = re.fullmatch(rf'({number}\.{number}\.{number})-beta\.([1-9][0-9]{{0,8}})', version)
    if not match:
        raise ValueError('Unsupported product version; expected canonical MAJOR.MINOR.PATCH-beta.N')
    base, beta = match.groups()
    return f'{base}~beta.{beta}', base, f'0.beta.{beta}'


PRODUCT_VERSION = parse_product_version(read_regular(ROOT, VERSION_SOURCE))
DEBIAN_VERSION, RPM_VERSION, RPM_RELEASE = beta_version_parts(PRODUCT_VERSION)


def package_filename(name, kind):
    if name not in PACKAGE_NAMES:
        raise ValueError(f'Unknown package: {name}')
    if kind == 'deb':
        return f'{name}_{DEBIAN_VERSION}_amd64.deb'
    if kind == 'rpm':
        return f'{name}-{RPM_VERSION}-{RPM_RELEASE}.x86_64.rpm'
    raise ValueError(f'Unknown package format: {kind}')


PACKAGE_ARTIFACTS = tuple(sorted(package_filename(name, kind)
                                 for name in PACKAGE_NAMES for kind in ('deb', 'rpm')))


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

    core, installed_by_source = {}, {}

    def add_core(source, target, mode=0o644):
        if target in core or source in installed_by_source:
            raise ValueError(f'Duplicate package source or destination: {source} -> {target}')
        core[target] = content(source, mode)
        installed_by_source[source] = target

    add_core('build/bin/virmill', 'usr/bin/virmill', 0o755)
    add_core('build/bin/virmilld', 'usr/bin/virmilld', 0o755)
    add_core('packaging/systemd/virmilld.service', 'usr/lib/systemd/user/virmilld.service')
    add_core('packaging/systemd/virmill-virtqemud.socket', 'usr/lib/systemd/user/virmill-virtqemud.socket')
    add_core('packaging/systemd/virmill-virtqemud.service', 'usr/lib/systemd/user/virmill-virtqemud.service')
    add_core('LICENSE', 'usr/share/licenses/virmill/LICENSE')
    add_core('packaging/completions/virmill.bash', 'usr/share/bash-completion/completions/virmill')
    add_core('packaging/completions/virmill.zsh', 'usr/share/zsh/site-functions/_virmill')
    add_core('packaging/completions/virmill.fish', 'usr/share/fish/vendor_completions.d/virmill.fish')
    add_core(PROTOCOL_SOURCE, PROTOCOL_TARGET)
    for path in sorted(tracked):
        for tree, target in SOURCE_TREES:
            if path.startswith(tree + '/') and PurePosixPath(path).suffix in SOURCE_SUFFIXES and 'evidence/logs' not in path:
                add_core(path, target + '/' + path[len(tree) + 1:])
    known_sources = set(tracked) | set(BINARY_PATHS)
    for source, target in installed_by_source.items():
        if source.endswith('.md'):
            data, mode = core[target]
            core[target] = (rewrite_markdown(source, target, data, installed_by_source, known_sources), mode)
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
    """A dpkg-style archive: './' names, with every parent directory before its files.

    dpkg cannot unpack a file whose directory is neither on disk nor in the archive.
    """
    output = io.BytesIO()
    directories = {parent for name in files for parent in PurePosixPath(name).parents} - {PurePosixPath('.')}
    with tarfile.open(fileobj=output, mode='w:xz', format=tarfile.USTAR_FORMAT) as archive:
        for name in ['./'] + [f'./{directory}/' for directory in sorted(directories, key=str)]:
            entry = tarfile.TarInfo(name)
            entry.type, entry.mode, entry.mtime = tarfile.DIRTYPE, 0o755, epoch
            archive.addfile(entry)
        for name, (data, mode) in sorted(files.items()):
            entry = tarfile.TarInfo('./' + name)
            entry.size, entry.mode, entry.mtime = len(data), mode, epoch
            archive.addfile(entry, io.BytesIO(data))
    return output.getvalue()


def ar_bytes(members, epoch):
    output = io.BytesIO()
    output.write(b'!<arch>\n')
    for name, data in members:
        header = f'{name:<16}{epoch:<12}{0:<6}{0:<6}{"100644":<8}{len(data):<10}`\n'
        assert len(header) == 60
        output.write(header.encode('ascii'))
        output.write(data)
        if len(data) % 2:
            output.write(b'\n')
    return output.getvalue()


def debian_docs(name, files, epoch):
    """Policy-required copyright and native changelog, from the packaged license."""
    license_path = f'usr/share/licenses/{name}/LICENSE'
    if license_path not in files:
        raise ValueError(f'{name} has no packaged license for its Debian copyright file')
    lines = files[license_path][0].decode().strip().splitlines()
    notice = next((line for line in lines if line.startswith('Copyright')), None)
    if notice is None:
        raise ValueError('LICENSE has no Copyright line for the Debian copyright file')
    body = lines[lines.index(notice) + 1:]
    while body and not body[0].strip():
        body.pop(0)
    holder = notice.removeprefix('Copyright').strip().removeprefix('(c)').strip()
    copyright = (f'Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/\n'
                 f'Upstream-Name: Virmill\nSource: {HOMEPAGE}\n\nFiles: *\nCopyright: {holder}\nLicense: Expat\n'
                 + ''.join(f' {line}\n' if line.strip() else ' .\n' for line in body))
    t = time.gmtime(epoch)
    day = ('Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun')[t.tm_wday]
    month = ('Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec')[t.tm_mon - 1]
    changelog = (f'{name} ({DEBIAN_VERSION}) unstable; urgency=medium\n\n'
                 f'  * Virmill {PRODUCT_VERSION} owner-test beta; incomplete and not\n    release-qualified.\n\n'
                 f' -- {MAINTAINER}  {day}, {t.tm_mday:02d} {month} {t.tm_year} {t.tm_hour:02d}:{t.tm_min:02d}:{t.tm_sec:02d} +0000\n')
    return {f'usr/share/doc/{name}/copyright': (copyright.encode(), 0o644),
            f'usr/share/doc/{name}/changelog.gz': (gzip.compress(changelog.encode(), compresslevel=9, mtime=0), 0o644)}


def deb_control(name, files):
    """Binary control file; Installed-Size counts KiB per file plus one per directory, like dpkg."""
    directories = {parent for path in files for parent in PurePosixPath(path).parents} - {PurePosixPath('.')}
    size = sum(-(-len(data) // 1024) for data, _ in files.values()) + len(directories)
    return (f'Package: {name}\nVersion: {DEBIAN_VERSION}\nArchitecture: amd64\nMaintainer: {MAINTAINER}\n'
            f'Installed-Size: {size}\nDepends: {DEB_DEPENDS[name]}\nSection: admin\nPriority: optional\n'
            f'Homepage: {HOMEPAGE}\nDescription: {DEB_SUMMARY[name]}\n'
            f' Owner-test beta {PRODUCT_VERSION}: incomplete and not release-qualified.\n'
            ' Installing it enables no service, host network or privilege setup.\n')


def md5sums(files):
    return ''.join(f'{hashlib.md5(data, usedforsecurity=False).hexdigest()}  {path}\n'
                   for path, (data, _) in sorted(files.items())).encode()


def commit_time(root):
    """Reproducible default timestamp: the checkout's commit time, or 0 without a commit."""
    result = subprocess.run(['git', 'show', '-s', '--format=%ct', 'HEAD'], cwd=root, capture_output=True, text=True)
    return int(result.stdout) if result.returncode == 0 and result.stdout.strip().isdigit() else 0


def copy_expected_rpm(root, name):
    filename = package_filename(name, 'rpm')
    data = read_regular(root, f'build/rpm/{name}/RPMS/x86_64/{filename}')
    write_regular(root, 'dist/' + filename, data)


def package_artifacts(root):
    return [{'path': filename, 'sha256': hashlib.sha256(read_regular(root, 'dist/' + filename)).hexdigest()}
            for filename in PACKAGE_ARTIFACTS]


def build_packages(root=ROOT):
    root = Path(root).absolute()
    tracked = tracked_inputs(root)
    if tracked.get(VERSION_SOURCE) not in (b'100644', b'100755'):
        raise ValueError('Product version source is not an indexed regular file')
    if parse_product_version(read_regular(root, VERSION_SOURCE)) != PRODUCT_VERSION:
        raise ValueError('Package source root version does not match the executing package builder')
    epoch = int(os.environ['SOURCE_DATE_EPOCH']) if 'SOURCE_DATE_EPOCH' in os.environ else commit_time(root)
    packages = collect_package_files(root)
    os.close(directory_fd(root / 'dist', create=True))
    for name, files in packages.items():
        stage = reset_owned_directory(root, f'build/package-stage/{name}')
        rpm = reset_owned_directory(root, f'build/rpm/{name}')
        description = f'Virmill {PRODUCT_VERSION} owner-test beta; incomplete and not release-qualified'
        deb_files = {**files, **debian_docs(name, files, epoch)}
        control = {'control': (deb_control(name, deb_files).encode(), 0o644), 'md5sums': (md5sums(deb_files), 0o644)}
        deb = ar_bytes([('debian-binary', b'2.0\n'),
                        ('control.tar.xz', tar_bytes(control, epoch)),
                        ('data.tar.xz', tar_bytes(deb_files, epoch))], epoch)
        write_regular(root, 'dist/' + package_filename(name, 'deb'), deb)
        for path, (data, mode) in files.items():
            write_regular(root, f'build/package-stage/{name}/' + path, data, mode)
        for folder in ('BUILD', 'BUILDROOT', 'SPECS', 'SOURCES', 'RPMS', 'SRPMS'):
            os.close(directory_fd(rpm / folder, create=True))
        filelist = '\n'.join(rpm_file_path('/' + path) for path in sorted(files))
        requirements = 'Requires: libvirt-libs, qemu-kvm, qemu-img, bubblewrap, util-linux, xorriso\n' if name == 'virmill' else 'Requires: libvirt-libs\n'
        spec = f'''Name: {name}
Version: {RPM_VERSION}
Release: {RPM_RELEASE}
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
