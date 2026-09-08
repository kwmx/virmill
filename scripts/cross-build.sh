#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
if [ "$#" -ne 0 ]; then
  printf '%s\n' 'cross-build.sh takes no arguments; every declared target is required' >&2
  exit 2
fi

# The declared compile-only targets are not additional supported VM hosts.
# Ignore caller/user Go settings, workspaces, experiments and tuning defaults.
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off CGO_ENABLED=0
export GOENV=off GOWORK=off GO111MODULE=on GOFLAGS= GOEXPERIMENT=
export GOAMD64=v1 GOARM64=v8.0 LC_ALL=C TZ=UTC
unset GOOS GOARCH GOROOT GOTOOLDIR
targets='darwin/arm64 windows/amd64'
packages='internal/domain internal/app/provision internal/wire internal/validation internal/operations internal/ui/cli internal/ui/tui'
for input in scripts/go go.mod vendor/modules.txt sdk/go/go.mod sdk/go/protocol/json.go sdk/go/example/summary.go examples/plugins/vm-summary/go.mod tests/fixtures/plugins/provider/go.mod tests/fixtures/plugins/provider/main.go; do
  if [ ! -s "$input" ]; then
    printf 'required cross-build input is missing or empty: %s\n' "$input" >&2
    exit 1
  fi
done
for package in $packages; do
  if [ ! -d "$package" ]; then
    printf 'required cross-build package is missing: %s\n' "$package" >&2
    exit 1
  fi
done
available=$(./scripts/go tool dist list)
for target in $targets; do
  found=no
  for candidate in $available; do
    if [ "$candidate" = "$target" ]; then found=yes; break; fi
  done
  if [ "$found" != yes ]; then
    printf 'pinned toolchain lacks required target: %s\n' "$target" >&2
    exit 1
  fi
done

# A new output directory prevents earlier artifacts from satisfying this run.
mkdir -p build/cross
out=$(mktemp -d "$root/build/cross/run.XXXXXX")
trap 'code=$?; if [ "$code" -ne 0 ]; then printf "Cross-build failed; partial outputs retained: %s\n" "$out" >&2; fi' 0
./scripts/go version > "$out/toolchain.txt"
printf '%s\n' 'GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off CGO_ENABLED=0 GOENV=off GOWORK=off GO111MODULE=on' 'GOFLAGS= GOEXPERIMENT= GOAMD64=v1 GOARM64=v8.0' 'flags: -trimpath -buildvcs=false -ldflags=-buildid=' 'root module: -mod=vendor; SDK and samples: -mod=readonly' > "$out/build-settings.txt"
: > "$out/artifacts.txt"
for target in $targets; do
  GOOS=${target%/*}
  GOARCH=${target#*/}
  export GOOS GOARCH
  printf 'Cross-building GOOS=%s GOARCH=%s CGO_ENABLED=0\n' "$GOOS" "$GOARCH"
  mkdir -p "$out/$target/core"
  for package in $packages; do
    name=$(printf '%s' "$package" | tr / -)
    artifact="$target/core/$name.a"
    ./scripts/go build -mod=vendor -trimpath -buildvcs=false -ldflags=-buildid= -o "$out/$artifact" "./$package"
    printf '%s\n' "$artifact" >> "$out/artifacts.txt"
  done
  for package in sdk protocol sdk-summary; do
    source=.
    if [ "$package" = protocol ]; then source=./protocol; fi
    if [ "$package" = sdk-summary ]; then source=./example; fi
    artifact="$target/$package.a"
    ./scripts/go -C sdk/go build -mod=readonly -trimpath -buildvcs=false -ldflags=-buildid= -o "$out/$artifact" "$source"
    printf '%s\n' "$artifact" >> "$out/artifacts.txt"
  done
  suffix=
  if [ "$GOOS" = windows ]; then suffix=.exe; fi
  artifact="$target/sdk.test$suffix"
  ./scripts/go -C sdk/go test -c -mod=readonly -trimpath -buildvcs=false -ldflags=-buildid= -o "$out/$artifact" .
  printf '%s\n' "$artifact" >> "$out/artifacts.txt"
  for sample in vm-summary provider-fixture; do
    case "$sample" in
      vm-summary) source=examples/plugins/vm-summary; package=. ;;
      provider-fixture) source=tests/fixtures/plugins/provider; package=. ;;
    esac
    artifact="$target/$sample$suffix"
    ./scripts/go -C "$source" build -mod=readonly -trimpath -buildvcs=false -ldflags=-buildid= -o "$out/$artifact" "$package"
    printf '%s\n' "$artifact" >> "$out/artifacts.txt"
  done
done

count=0
while IFS= read -r artifact; do
  if [ ! -f "$out/$artifact" ] || [ -L "$out/$artifact" ] || [ ! -s "$out/$artifact" ]; then
    printf 'required cross-build artifact is missing, empty or not regular: %s\n' "$artifact" >&2
    exit 1
  fi
  count=$((count + 1))
done < "$out/artifacts.txt"
if [ "$count" -ne 26 ]; then
  printf 'cross-build matrix incomplete: expected 26 artifacts, observed %s\n' "$count" >&2
  exit 1
fi
(cd "$out" && while IFS= read -r artifact; do sha256sum "$artifact"; done < artifacts.txt) > "$out/artifact-sha256.txt"
printf 'Cross-build passed: 2 targets, 26 compile-only artifacts; no target binaries executed\nOutputs: %s\n' "$out"
