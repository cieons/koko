GO ?= go
NPM ?= npm
# A release publishes the CLI, koko, for each of RELEASE_TARGETS, and the
# koko server, kokod, for each of KOKOD_TARGETS, named <program>-<os>-<arch>
# (ADR 0026, ADR 0040).
RELEASE_TARGETS := linux/amd64 linux/arm64 darwin/arm64
KOKOD_TARGETS := linux/amd64 linux/arm64
RELEASE_BINARIES := $(foreach t,$(RELEASE_TARGETS),koko-$(subst /,-,$(t))) \
	$(foreach t,$(KOKOD_TARGETS),kokod-$(subst /,-,$(t)))
# windows/amd64 is compiled only, never released: it keeps the CLI ready for
# Windows (P1) by failing the build when code skips its per-OS interface.
CROSS_TARGETS := $(RELEASE_TARGETS) windows/amd64
# npm ci writes this file, so it is newer than the lockfile once installed.
WEB_DEPS := web/node_modules/.package-lock.json

.PHONY: check lint fmt vet test cross build dist image webui generate clean

## check: everything a change must pass before it is committed
check: webui lint vet test cross
# The cross builds and the tests embed what make webui writes, so check runs
# its steps in order, even under -j.
.NOTPARALLEL: check

# golangci-lint checks only the files the build for one OS takes, so lint
# runs once for each OS koko builds for: a _linux.go file is linted on macOS
# too, and a _darwin.go or _windows.go file on Linux.
LINT_OSES := $(sort $(foreach t,$(CROSS_TARGETS),$(word 1,$(subst /, ,$(t)))))

lint:
	@for os in $(LINT_OSES); do \
		echo "lint GOOS=$$os"; \
		GOOS=$$os golangci-lint run || exit 1; \
	done

fmt:
	golangci-lint fmt

vet:
	$(GO) vet ./...

# Tests run under a throwaway HOME so they never touch the real ~/.ssh,
# ~/.claude or ~/.config. Go's caches and env file are pinned first, or
# every run would re-download modules into the temp HOME. The Go tests
# include the check that the web UI's generated types are up to date; the
# web UI's own tests run in Vitest. A test binary still running after 5
# minutes has hung: go test fails it with a goroutine dump, well inside the
# CI job's timeout.
test: $(WEB_DEPS)
	@home=$$(mktemp -d); trap 'rm -rf "$$home"' EXIT; \
	export GOENV="$$($(GO) env GOENV)" GOPATH="$$($(GO) env GOPATH)" \
	GOMODCACHE="$$($(GO) env GOMODCACHE)" GOCACHE="$$($(GO) env GOCACHE)" \
	HOME="$$home" XDG_CONFIG_HOME="$$home/.config" \
	XDG_DATA_HOME="$$home/.local/share" XDG_STATE_HOME="$$home/.local/state"; \
	$(GO) test -race -timeout 5m ./... && cd web && node_modules/.bin/vitest run

cross:
	@for t in $(CROSS_TARGETS); do \
		echo "build $$t"; \
		GOOS=$${t%/*} GOARCH=$${t#*/} CGO_ENABLED=0 $(GO) build -o /dev/null ./... || exit 1; \
	done

build: webui
	$(GO) build -o bin/koko ./cmd/koko
	$(GO) build -o bin/kokod ./cmd/kokod

## dist: build a release's assets into dist/: the binaries (kokod-linux-<arch>
## carries the koko server and its web UI), install.sh, and SHA256SUMS
## listing them (docs/subsystems/release.md). Go stamps each binary's version
## from git, so at a tag, with nothing changed, the version is the tag.
dist: webui
	@mkdir -p dist
	@for b in $(RELEASE_BINARIES); do \
		prog=$${b%%-*}; p=$${b#*-}; echo "build dist/$$b"; \
		GOOS=$${p%-*} GOARCH=$${p#*-} CGO_ENABLED=0 \
			$(GO) build -trimpath -ldflags='-s -w' -o dist/$$b ./cmd/$$prog || exit 1; \
	done
	cp release/install.sh dist/install.sh
	cd dist && sha256sum $(RELEASE_BINARIES) install.sh >SHA256SUMS

## image: build the koko server's image (release/Dockerfile) for
## IMAGE_PLATFORMS from the binaries make dist built, into dist/koko-image.tar,
## an OCI layout archive that the release pushes to ghcr.io/cieons/koko and no
## SHA256SUMS lists (docs/subsystems/release.md). It needs Docker with buildx,
## and builder koko-image, which it makes once with the docker-container
## driver, the one that exports a multi-platform image. Nothing runs in the
## image, so no emulation is needed; make check never runs it.
IMAGE_PLATFORMS := linux/amd64,linux/arm64
image:
	@docker buildx inspect koko-image >/dev/null 2>&1 || \
		docker buildx create --name koko-image --driver docker-container >/dev/null
	docker buildx build --builder koko-image --platform $(IMAGE_PLATFORMS) \
		--provenance=false --sbom=false \
		--build-arg VERSION=$(patsubst v%,%,$(shell git describe --tags --always --dirty)) \
		--build-arg REVISION=$(shell git rev-parse HEAD) \
		--file release/Dockerfile --output type=oci,dest=dist/koko-image.tar dist

## webui: build the web UI into internal/server/webui/site/app, which kokod,
## and koko while it still carries the koko server, embed; a build without it
## embeds the placeholder page (ADR 0023)
webui: $(WEB_DEPS)
	cd web && $(NPM) run build

$(WEB_DEPS): web/package.json web/package-lock.json
	cd web && $(NPM) ci
	@touch $@

## generate: rewrite the web UI's TypeScript types (web/src/api) from the
## API's Go types (internal/shared/api)
generate:
	$(GO) test ./internal/shared/api -run '^TestTypeScriptTypes$$' -update

clean:
	rm -rf bin dist internal/server/webui/site/app
