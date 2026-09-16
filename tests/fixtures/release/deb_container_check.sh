#!/bin/sh
# Install-test built DEBs in throwaway Debian 13 and Ubuntu 24.04 containers.
#
# Usage: deb_container_check.sh CORE_DEB HELPER_DEB
#
# Needs rootless podman and network access for images and apt. Each distro gets
# a read-only copy of exactly these two DEBs. It checks them with dpkg-deb and lintian,
# installs them with apt, verifies files and systemd units, runs the CLI as an
# ordinary user, then removes and purges them. Images this script pulls are
# removed again. Containers are not a host install matrix.
set -eu
[ "$#" = 2 ] && [ -f "$1" ] && [ -f "$2" ] || { echo "usage: $0 CORE_DEB HELPER_DEB" >&2; exit 2; }
core=$(cd "$(dirname "$1")" && pwd)/$(basename "$1")
helper=$(cd "$(dirname "$2")" && pwd)/$(basename "$2")
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cat > "$work/inner.sh" <<'EOS'
set -u
export DEBIAN_FRONTEND=noninteractive
cd /pkgs
set -- virmill_*_amd64.deb; [ "$#" = 1 ] || { echo "FAIL: expected one core DEB, found: $*"; exit 1; }; core=$1
set -- virmill-host-helper_*_amd64.deb; [ "$#" = 1 ] || { echo "FAIL: expected one helper DEB, found: $*"; exit 1; }; helper=$1
failed=0
fail() { echo "FAIL: $*"; failed=1; }
. /etc/os-release; echo "== $PRETTY_NAME"
# Minimized images skip /usr/share/doc; restore normal dpkg unpacking.
if [ -e /etc/dpkg/dpkg.cfg.d/excludes ]; then rm /etc/dpkg/dpkg.cfg.d/excludes; echo "removed image dpkg path excludes"; fi
apt-get update -qq >/dev/null && apt-get install -y -qq --no-install-recommends lintian systemd >/dev/null 2>&1 || fail "tool installation"
for deb in "$core" "$helper"; do
  dpkg-deb --info "$deb" | sed 's/^/  /'
  dpkg-deb --ctrl-tarfile "$deb" >/dev/null && dpkg-deb --fsys-tarfile "$deb" >/dev/null || fail "dpkg-deb cannot read $deb"
done
echo "== lintian $(lintian --version)"
lintian --tag-display-limit 0 -I -E --pedantic "$core" "$helper" > /tmp/lintian.txt 2>/dev/null || true
awk '/^[EWIPXCON]: /{print $1, $2, $3}' /tmp/lintian.txt | sort | uniq -c | sed 's/^/  /'
echo "== install"
apt-get install -y "./$core" "./$helper" > /tmp/install.log 2>&1 || { tail -20 /tmp/install.log; fail "apt install"; }
dpkg-query -W -f='  ${Package} ${Version} ${db:Status-Abbrev}\n' virmill virmill-host-helper
dpkg --verify virmill virmill-host-helper && echo "dpkg --verify: ok" || fail "dpkg --verify"
useradd -m tester && su - tester -c 'virmill version --output json' && echo || fail "virmill version as an ordinary user"
# The per-user libvirt units (ADR 0064) start a daemon Virmill does not ship,
# and systemd-analyze refuses a unit whose command is missing. Where libvirt is
# not installed their syntax and wiring are checked against a stand-in that is
# removed again; on a real host without it the socket skips itself. Newer
# systemd also runs `man` for each Documentation= page, and the only pages these
# units name are libvirt's, which Virmill neither ships nor depends on.
stub=""
[ -x /usr/sbin/virtqemud ] || { stub=/usr/sbin/virtqemud; printf '#!/bin/sh\nexit 0\n' > "$stub"; chmod 0755 "$stub"; }
systemd-analyze verify --man=no /usr/lib/systemd/system/virmill-host-helper.socket /usr/lib/systemd/system/virmill-host-helper.service \
  /usr/lib/systemd/user/virmilld.service /usr/lib/systemd/user/virmill-virtqemud.socket \
  /usr/lib/systemd/user/virmill-virtqemud.service && echo "systemd units: ok" || fail "systemd-analyze verify"
[ -z "$stub" ] || rm -f "$stub"
echo "owned directories: $(dpkg -L virmill virmill-host-helper | while read -r p; do [ -d "$p" ] && echo; done | wc -l)"
apt-get remove -y virmill virmill-host-helper > /dev/null 2>&1 || fail "apt remove"
dpkg --purge virmill virmill-host-helper > /dev/null 2>&1
left=""
for p in /usr/bin/virmill /usr/bin/virmilld /usr/libexec/virmill-host-helper /usr/share/doc/virmill /usr/share/virmill \
         /usr/share/doc/virmill-host-helper /usr/share/licenses/virmill /usr/share/licenses/virmill-host-helper; do
  [ -e "$p" ] && left="$left $p"
done
[ -z "$left" ] && echo "purge left nothing behind" || fail "left after purge:$left"
[ "$failed" = 0 ] && echo "RESULT: passed" || { echo "RESULT: failed"; exit 1; }
EOS

status=0
for image in docker.io/library/debian:13 docker.io/library/ubuntu:24.04; do
  dir="$work/$(printf '%s' "$image" | tr -c 'a-z0-9' '_')"
  mkdir "$dir"
  cp "$core" "$helper" "$work/inner.sh" "$dir/"
  pulled=no
  podman image exists "$image" || pulled=yes
  echo "######## $image"
  podman run --rm -v "$dir:/pkgs:ro,Z" "$image" timeout 1800 bash /pkgs/inner.sh || status=1
  if [ "$pulled" = yes ]; then podman rmi "$image" > /dev/null; fi
done
exit "$status"
