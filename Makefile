.PHONY: build test verify reference packages release-check
build:
	./scripts/build.sh
test:
	GOPROXY=off ./scripts/go test -mod=vendor -tags libvirt_dlopen ./...
	cd sdk/go && ../../scripts/go test ./...
verify: test
	GOPROXY=off ./scripts/go vet -mod=vendor -tags libvirt_dlopen ./...
	python3 scripts/traceability.py --check
reference: build
packages: build
	python3 scripts/package.py
release-check:
	python3 scripts/traceability.py --release
