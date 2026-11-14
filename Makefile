GO ?= go
NPM ?= npm
# A release publishes the CLI, koko, for each of RELEASE_TARGETS, and the
# koko server, kokod, for each of KOKOD_TARGETS, named <program>-<os>-<arch>,
# with .exe on Windows, which runs a file by its extension (ADR 0026, ADR
# 0040, ADR 0052).
RELEASE_TARGETS := linux/amd64 linux/arm64 darwin/arm64 windows/amd64
KOKOD_TARGETS := linux/amd64 linux/arm64
asset = $(1)-$(subst /,-,$(2))$(if $(filter windows/%,$(2)),.exe)
RELEASE_BINARIES := $(foreach t,$(RELEASE_TARGETS),$(call asset,koko,$(t))) \
	$(foreach t,$(KOKOD_TARGETS),$(call asset,kokod,$(t)))
# The install scripts a release publishes beside the binaries: install.sh
# for Linux and macOS, install.ps1 for Windows.
RELEASE_SCRIPTS := install.sh install.ps1
# make cross compiles every package for each target, cmd/kokod's stub on
# macOS and Windows included.
CROSS_TARGETS := $(RELEASE_TARGETS)
# npm ci writes this file, so it is newer than the lockfile once installed.
WEB_DEPS := web/node_modules/.package-lock.json

.PHONY: check lint fmt vet test stress cross build dist image webui generate vt-wasm clean

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
# every run would re-download modules into the temp HOME. TEST_HOME makes
# the HOME, removed when the recipe's shell exits, and exports it.
TEST_HOME = home=$$(mktemp -d); trap 'rm -rf "$$home"' EXIT; \
	export GOENV="$$($(GO) env GOENV)" GOPATH="$$($(GO) env GOPATH)" \
	GOMODCACHE="$$($(GO) env GOMODCACHE)" GOCACHE="$$($(GO) env GOCACHE)" \
	HOME="$$home" XDG_CONFIG_HOME="$$home/.config" \
	XDG_DATA_HOME="$$home/.local/share" XDG_STATE_HOME="$$home/.local/state";

# The Go tests include the check that the web UI's generated types are up
# to date; the web UI's own tests run in Vitest. A test binary still
# running after 5 minutes has hung: go test fails it with a goroutine dump,
# well inside the CI job's timeout.
test: $(WEB_DEPS)
	@$(TEST_HOME) $(GO) test -race -timeout 5m ./... && cd web && node_modules/.bin/vitest run

## stress: reproduce a flake, and prove its fix, by running the tests
## STRESS_RUN matches in STRESS_PKGS under load, as on CI's slower runners:
##   make stress STRESS_PKGS=./internal/client/cli STRESS_RUN='^TestX$$'
## STRESS_RUN is passed to go test as it is written, a $$ included.
## Each runs STRESS_COUNT times at each GOMAXPROCS in STRESS_CPU, under the
## race detector, beside STRESS_LOAD busy loops, two for each CPU by
## default (docs/code-standards.md, "Tests").
STRESS_PKGS ?=
STRESS_RUN ?=
STRESS_COUNT ?= 20
STRESS_CPU ?= 1,4
STRESS_LOAD ?= $$(( 2 * $$(getconf _NPROCESSORS_ONLN) ))
stress:
	@[ -n "$(STRESS_PKGS)" ] && [ -n '$(value STRESS_RUN)' ] || \
		{ echo "make stress needs STRESS_PKGS and STRESS_RUN, such as STRESS_PKGS=./internal/client/cli STRESS_RUN='^TestX\$$'" >&2; exit 2; }
	@$(TEST_HOME) load=; \
	trap '{ kill $$load; wait; } 2>/dev/null; rm -rf "$$home"' EXIT; \
	for _ in $$(seq $(STRESS_LOAD)); do \
		sh -c 'while :; do :; done' & load="$$load $$!"; \
	done; \
	$(GO) test -race -count=$(STRESS_COUNT) -cpu=$(STRESS_CPU) -run '$(value STRESS_RUN)' $(STRESS_PKGS)

cross:
	@for t in $(CROSS_TARGETS); do \
		echo "build $$t"; \
		GOOS=$${t%/*} GOARCH=$${t#*/} CGO_ENABLED=0 $(GO) build -o /dev/null ./... || exit 1; \
	done

build: webui
	$(GO) build -o bin/koko ./cmd/koko
	$(GO) build -o bin/kokod ./cmd/kokod

## dist: build a release's assets into dist/: the binaries (kokod-linux-<arch>
## carries the koko server and its web UI), install.sh, install.ps1, and
## SHA256SUMS listing them (docs/subsystems/release.md). Go stamps each
## binary's version from git, so at a tag, with nothing changed, the version
## is the tag.
dist: webui
	@mkdir -p dist
	@for b in $(RELEASE_BINARIES); do \
		prog=$${b%%-*}; p=$${b#*-}; p=$${p%.exe}; echo "build dist/$$b"; \
		GOOS=$${p%-*} GOARCH=$${p#*-} CGO_ENABLED=0 \
			$(GO) build -trimpath -ldflags='-s -w' -o dist/$$b ./cmd/$$prog || exit 1; \
	done
	cp $(addprefix release/,$(RELEASE_SCRIPTS)) dist/
	cd dist && sha256sum $(RELEASE_BINARIES) $(RELEASE_SCRIPTS) >SHA256SUMS

## image: build the koko server's image (release/Dockerfile) for
## IMAGE_PLATFORMS from the binaries make dist built, into dist/koko-image.tar,
## an OCI layout archive that the release pushes to ghcr.io/cieons/koko and no
## SHA256SUMS lists (docs/subsystems/release.md), with kokod's third-party
## notices from its build context notices. It needs Docker with buildx,
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
		--build-context notices=internal/server/kokod \
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

## vt-wasm: fetch libghostty-vt's WebAssembly module, the release file
## internal/server/vt/ghostty-vt.wasm.sum names, into ghostty-vt.wasm beside
## it, checked against the SHA-256 there (ADR 0058). Pinning a new release
## is a new line in the .sum file and this target; make check checks the
## committed file against the line (TestTheModuleIsThePinnedRelease).
VT_WASM := internal/server/vt/ghostty-vt.wasm
vt-wasm:
	@set -e; read -r sum file <$(VT_WASM).sum; \
	version=$${file#ghostty-vt-}; version=$${version%%-*}; version=$${version%.wasm}; \
	tmp=$$(mktemp); trap 'rm -f "$$tmp"' EXIT; \
	curl -fsSL -o "$$tmp" "https://release.files.ghostty.org/libghostty-vt/$$version/$$file"; \
	echo "$$sum  $$tmp" | shasum -a 256 -c - >/dev/null || \
		{ echo "$$file does not match $(VT_WASM).sum" >&2; exit 1; }; \
	cp "$$tmp" $(VT_WASM); echo "$(VT_WASM): $$file"

clean:
	rm -rf bin dist internal/server/webui/site/app
