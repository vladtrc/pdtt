BIN := ./bin/pdtt
EXAMPLE ?= 40-shape-morph
FPS ?= 30
W ?= 960
H ?= 540

.PHONY: build render example clean manim-ref

build:
	mkdir -p ./bin
	go build -o $(BIN) ./cmd/pdtt

render: build
	$(BIN) -i examples/$(EXAMPLE)/run.pdtt -o examples/$(EXAMPLE)/res -fps $(FPS) -w $(W) -h $(H)

example: render

manim-ref:
	@if command -v manim >/dev/null 2>&1; then \
		mkdir -p examples/$(EXAMPLE)/ref; \
		manim -qk examples/$(EXAMPLE)/ref.py -o ref --media_dir examples/$(EXAMPLE)/ref; \
	else \
		echo "manim not found; skipping reference render"; \
	fi

clean:
	rm -rf ./bin examples/*/res
