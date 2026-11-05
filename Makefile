GO ?= go
NPM ?= npm
# windows/amd64 is compiled only, never released: it keeps the CLI ready for
# Windows (P1) by failing the build when code skips its per-OS interface.
CROSS_TARGETS := linux/amd64 linux/arm64 darwin/arm64 windows/amd64
# npm ci writes this file, so it is newer than the lockfile once installed.
WEB_DEPS := web/node_modules/.package-lock.json

.PHONY: check lint fmt vet test cross build webui generate clean

## check: everything a change must pass before it is committed
check: webui lint vet test cross
# The cross builds and the tests embed what make webui writes, so check runs
# its steps in order, even under -j.
.NOTPARALLEL: check

lint:
	golangci-lint run

fmt:
	golangci-lint fmt

vet:
	$(GO) vet ./...

# Tests run under a throwaway HOME so they never touch the real ~/.ssh,
# ~/.claude or ~/.config. Go's caches and env file are pinned first, or
# every run would re-download modules into the temp HOME. The Go tests
# include the check that the web UI's generated types are up to date; the
# web UI's own tests run in Vitest.
test: $(WEB_DEPS)
	@home=$$(mktemp -d); trap 'rm -rf "$$home"' EXIT; \
	export GOENV="$$($(GO) env GOENV)" GOPATH="$$($(GO) env GOPATH)" \
	GOMODCACHE="$$($(GO) env GOMODCACHE)" GOCACHE="$$($(GO) env GOCACHE)" \
	HOME="$$home" XDG_CONFIG_HOME="$$home/.config" \
	XDG_DATA_HOME="$$home/.local/share" XDG_STATE_HOME="$$home/.local/state"; \
	$(GO) test -race ./... && cd web && node_modules/.bin/vitest run

cross:
	@for t in $(CROSS_TARGETS); do \
		echo "build $$t"; \
		GOOS=$${t%/*} GOARCH=$${t#*/} CGO_ENABLED=0 $(GO) build -o /dev/null ./... || exit 1; \
	done

build: webui
	$(GO) build -o bin/koko ./cmd/koko

## webui: build the web UI into internal/server/webui/site/app, which the koko
## binary embeds; a build without it embeds the placeholder page (ADR 0023)
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
