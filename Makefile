# A thin layer over the Go toolchain: Go decides what to rebuild and caches
# it; these targets only name the steps. `make check` is the one gate, locally
# and in CI; it never rewrites sources (`make fmt` does), its only output is
# the ignored bin/ it builds last.

GO      ?= go
TOOL    := $(GO) tool -modfile=tools/go.mod
BIN     := bin/cc-harness-agents
# The resume bridge is a script; bin/ gets a copy so it finds the binary as
# its sibling, exactly as it will on PATH.
RESUME  := bin/cc-harness-resume
# The shim gets a directory of its own: that directory goes ahead of the
# native launcher on PATH, and nothing else in bin/ is named claude.
SHIM    := bin/shim/claude
SH      := install.sh tests/run tests/test-install.sh cmd/claude-shim/claude
BASH    := tests/assert.sh tests/test-cc-harness-agents.sh tests/test-claude-wrapper.sh
SHFMT   := -i 2 -ci -sr

.PHONY: build check lint test fmt clean

# Replaced atomically, so a failed build never destroys a working binary.
build:
	$(GO) build -o $(BIN).tmp ./cmd/cc-harness-agents
	mv -f $(BIN).tmp $(BIN)
	cp cmd/cc-harness-resume/cc-harness-resume $(RESUME).tmp
	mv -f $(RESUME).tmp $(RESUME)
	mkdir -p bin/shim
	cp cmd/claude-shim/claude $(SHIM).tmp
	mv -f $(SHIM).tmp $(SHIM)

check: lint test build

lint:
	@unformatted="$$(gofmt -l cmd internal tests)" || exit 1; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed (run make fmt):"; echo "$$unformatted"; exit 1; fi
	$(GO) vet ./...
	$(TOOL) staticcheck ./...
	shellcheck -s sh $(SH)
	shellcheck -s bash $(BASH)
	$(TOOL) shfmt -d $(SHFMT) $(SH) $(BASH)
	zsh -n share/claude.zsh

test:
	GO="$(GO)" $(GO) test -race ./...
	GO="$(GO)" tests/run

fmt:
	gofmt -w cmd internal tests
	$(TOOL) shfmt -w $(SHFMT) $(SH) $(BASH)

clean:
	rm -f $(BIN) $(BIN).tmp $(RESUME) $(RESUME).tmp $(SHIM) $(SHIM).tmp
