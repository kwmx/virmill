#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
export GOTOOLCHAIN=local
export GOPROXY=off
export GOSUMDB=off
export CGO_ENABLED=1
export LC_ALL=C
revision=$(git rev-parse HEAD)
epoch=${SOURCE_DATE_EPOCH:-0}
mkdir -p build/bin
printf 'module virmill.local/build-artifacts\n\ngo 1.27.1\n' > build/go.mod
for command in virmill virmilld virmill-host-helper; do
  ./scripts/go build -mod=vendor -buildvcs=false -trimpath -tags libvirt_dlopen -ldflags "-buildid= -X virmill.local/core/internal/buildinfo.Revision=$revision -X virmill.local/core/internal/buildinfo.BuildTime=$epoch" -o "build/bin/$command" "./cmd/$command"
done
build/bin/virmill reference > docs/cli-reference.md
mkdir -p packaging/completions
for shell in bash zsh fish powershell; do
  build/bin/virmill completion "$shell" > "packaging/completions/virmill.$shell"
done
sha256sum build/bin/virmill build/bin/virmilld build/bin/virmill-host-helper > build/binary-checksums.txt
