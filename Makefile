BIN := ./bin/pdtt
FPS ?= 30
W ?= 960
H ?= 540

# every directory under examples/ that has a scene is a renderable example
EXAMPLES := $(notdir $(wildcard examples/*))

# NB: render-%/ref-% are intentionally NOT .PHONY — GNU Make skips pattern-rule
# search for phony targets, and there is no file by those names anyway.
.PHONY: all build render-all ref render example clean

# default: build once, then render every example's res/ in PARALLEL.
all:
	@$(MAKE) build
	@$(MAKE) -j$(words $(EXAMPLES)) render-all

render-all: $(addprefix render-,$(EXAMPLES))

build:
	mkdir -p ./bin
	go build -o $(BIN) ./cmd/pdtt

# render-<name>: render one example into examples/<name>/res
render-%: | build
	$(BIN) -i examples/$*/run.pdtt -o examples/$*/res -fps $(FPS) -w $(W) -h $(H)

# back-compat single-example entry points: `make render EXAMPLE=40-shape-morph`
EXAMPLE ?= 40-shape-morph
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
