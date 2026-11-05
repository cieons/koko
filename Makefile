GO ?= go
# windows/amd64 is compiled only, never released: it keeps the CLI ready for
# Windows (P1) by failing the build when code skips its per-OS interface.
CROSS_TARGETS := linux/amd64 linux/arm64 darwin/arm64 windows/amd64

.PHONY: check lint fmt vet test cross build clean

## check: everything a change must pass before it is committed
check: lint vet test cross

lint:
	golangci-lint run

fmt:
	golangci-lint fmt

vet:
	$(GO) vet ./...

# Tests run under a throwaway HOME so they never touch the real ~/.ssh,
# ~/.claude or ~/.config. Go's caches and env file are pinned first, or
# every run would re-download modules into the temp HOME.
test:
	@home=$$(mktemp -d); trap 'rm -rf "$$home"' EXIT; \
	GOENV="$$($(GO) env GOENV)" GOPATH="$$($(GO) env GOPATH)" \
	GOMODCACHE="$$($(GO) env GOMODCACHE)" GOCACHE="$$($(GO) env GOCACHE)" \
	HOME="$$home" XDG_CONFIG_HOME="$$home/.config" \
	XDG_DATA_HOME="$$home/.local/share" XDG_STATE_HOME="$$home/.local/state" \
	$(GO) test -race ./...

cross:
	@for t in $(CROSS_TARGETS); do \
		echo "build $$t"; \
		GOOS=$${t%/*} GOARCH=$${t#*/} CGO_ENABLED=0 $(GO) build -o /dev/null ./... || exit 1; \
	done

build:
	$(GO) build -o bin/koko ./cmd/koko

clean:
	rm -rf bin dist
