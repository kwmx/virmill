#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
export GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off
mkdir -p build/provider-conformance
./scripts/go -C tests/fixtures/plugins/provider build -mod=mod -buildvcs=false -trimpath -o "$root/build/provider-fixture" .
cp build/provider-fixture build/provider-conformance/provider-fixture
python3 - <<'PY'
import hashlib, json
from pathlib import Path
root = Path('build/provider-conformance')
manifest = json.loads(Path('tests/fixtures/plugins/provider/manifest.json').read_text())
manifest['entrypoints']['linux/amd64']['sha256'] = hashlib.sha256((root / 'provider-fixture').read_bytes()).hexdigest()
(root / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
print('Built confined simulated-provider workspace: build/provider-conformance')
PY
