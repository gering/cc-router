# A thin layer over the Go toolchain: Go decides what to rebuild and caches
# it; these targets only name the steps. `make check` is the one gate, locally
# and in CI, and never modifies the tree (`make fmt` does).

GO      ?= go
TOOL    := $(GO) tool -modfile=tools/go.mod
BIN     := bin/cc-harness-agents
SH      := install.sh tests/run tests/test-install.sh
BASH    := tests/test-cc-harness-agents.sh
SHFMT   := -i 2 -ci -sr

.PHONY: build check lint test fmt clean

# Replaced atomically, so a failed build never destroys a working binary.
build:
	$(GO) build -o $(BIN).tmp ./cmd/cc-harness-agents
	mv -f $(BIN).tmp $(BIN)

check: lint test build

lint:
	@unformatted="$$(gofmt -l cmd internal tests)"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed (run make fmt):"; echo "$$unformatted"; exit 1; fi
	$(GO) vet ./...
	$(TOOL) staticcheck ./...
	shellcheck -s sh $(SH)
	shellcheck -s bash $(BASH)
	$(TOOL) shfmt -d $(SHFMT) $(SH) $(BASH)

test:
	GO="$(GO)" $(GO) test -race ./...
	GO="$(GO)" tests/run

fmt:
	gofmt -w cmd internal tests
	$(TOOL) shfmt -w $(SHFMT) $(SH) $(BASH)

clean:
	rm -f $(BIN) $(BIN).tmp
