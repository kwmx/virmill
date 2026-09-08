#!/bin/sh
# Build both Go fixtures required by the opt-in confined protocol suite.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off GOENV=off GOFLAGS=
export GOOS=linux GOARCH=amd64 CGO_ENABLED=0
python3 - <<'PY'
import json
from pathlib import Path

root = Path('build/sdk-summary')
root.mkdir(parents=True, exist_ok=True)
(root / 'main.go').write_text('''package main
import ("context"; "fmt"; "os"; "virmill.local/sdk/example")
func main() {
    if err := example.New("example.virmill.sdk-summary").Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
''')
(root / 'go.mod').write_text('''module virmill.local/sdk-summary-fixture

go 1.27.1

require virmill.local/sdk v0.1.0
replace virmill.local/sdk => ../../sdk/go
''')
manifest = json.loads(Path('examples/plugins/vm-summary/manifest.json').read_text())
manifest['id'] = 'example.virmill.sdk-summary'
manifest['entrypoints'] = {'linux/amd64': {'path': 'vm-summary'}}
(root / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
PY
./scripts/go -C build/sdk-summary build -mod=mod -buildvcs=false -trimpath -o vm-summary .
./scripts/build-provider-fixture.sh
