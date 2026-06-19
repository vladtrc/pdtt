BIN := ./bin/pdtt
WEB_BIN := ./bin/pdttweb
TEMPL := github.com/a-h/templ/cmd/templ
FPS ?= 30
W ?= 960
H ?= 540
GO_FILES := $(shell git ls-files '*.go')
GO_LINT_PACKAGES := ./cmd/... ./internal/... ./pkg/...
GOFUMPT := go run mvdan.cc/gofumpt@latest
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

# every directory under examples/ that has a scene is a renderable example
EXAMPLES := $(notdir $(wildcard examples/*))

# NB: render-%/ref-% are intentionally NOT .PHONY — GNU Make skips pattern-rule
# search for phony targets, and there is no file by those names anyway.
.PHONY: all build web-build web-generate web-run tools format lint fmt render-all ref render example clean

# default: build once, then render every example's res/ in PARALLEL.
all:
	@$(MAKE) build
	@$(MAKE) -j$(words $(EXAMPLES)) render-all

render-all: $(addprefix render-,$(EXAMPLES))

build:
	mkdir -p ./bin
	go build -o $(BIN) ./cmd/pdtt

# regenerate templ views (internal/web/views_templ.go) from .templ sources.
# templ is a tool dependency (see `tool` directive in go.mod).
web-generate:
	go tool $(TEMPL) generate

# build the web-server entrypoint (same repo, different main).
# views_templ.go is checked in, so this does not force a regenerate.
web-build:
	mkdir -p ./bin
	go build -o $(WEB_BIN) ./cmd/pdttweb

# run the web server locally against the checked-in config + secret
web-run: web-build
	@mkdir -p data/videos data/work
	$(WEB_BIN) -config config.yaml -secret utils/secret/.secret

tools:
	go install mvdan.cc/gofumpt@latest
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

format:
	$(GOFUMPT) -w $(GO_FILES)

lint:
	go vet ./...
	$(GOLANGCI_LINT) run $(GO_LINT_PACKAGES)

fmt: format lint

# render-<name>: render one example into examples/<name>/res
render-%: | build
	$(BIN) -i examples/$*/run.pdtt -o examples/$*/res -fps $(FPS) -w $(W) -h $(H)

# back-compat single-example entry points: `make render EXAMPLE=shape-morph`
EXAMPLE ?= shape-morph
render: render-$(EXAMPLE)
example: render

# ref / ref-<name>: render the manim reference scenes (ref.py) via uv, in
# PARALLEL, into examples/<name>/ref. Best-effort; needs `uv`.
ref:
	@$(MAKE) -j$(words $(EXAMPLES)) $(addprefix ref-,$(EXAMPLES))

ref-%:
	./scripts/ref.sh examples/$*

clean:
	rm -rf ./bin examples/*/res examples/*/ref
