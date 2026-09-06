#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
export GOPROXY=off
export CGO_ENABLED=0
mkdir -p build/cross
for target in darwin/arm64 windows/amd64; do
  GOOS=${target%/*}
  GOARCH=${target#*/}
  export GOOS GOARCH
  ./scripts/go build -mod=vendor ./internal/domain ./internal/wire ./internal/validation ./internal/operations ./internal/ui/cli ./internal/ui/tui
  (cd sdk/go && ../../scripts/go test -c -o "../../build/cross/sdk-$GOOS-$GOARCH.test" .)
  (cd examples/plugins/vm-summary && ../../../scripts/go build -o "../../../build/cross/vm-summary-$GOOS-$GOARCH" .)
done
