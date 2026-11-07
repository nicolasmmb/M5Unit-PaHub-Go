TARGET ?= m5stick-c
PORT ?=
TINYGO ?= tinygo
COUNT ?= 8
BENCH ?= .
BENCHTIME ?= 1s

.PHONY: test vet fmt check bench bench-save bench-compare alloc footprint escape profile examples agnostic

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

check: vet test alloc examples

bench:
	COUNT=$(COUNT) BENCH=$(BENCH) BENCHTIME=$(BENCHTIME) ./scripts/bench.sh run

bench-save:
	COUNT=$(COUNT) BENCH=$(BENCH) BENCHTIME=$(BENCHTIME) ./scripts/bench.sh save $(NAME)

bench-compare:
	./scripts/bench.sh compare $(FROM) $(TO)

alloc:
	./scripts/bench.sh alloc

footprint:
	go test -run TestFootprint -v

escape:
	./scripts/bench.sh escape

profile:
	./scripts/bench.sh profile

examples:
	$(TINYGO) build -target=$(TARGET) -size short -o /tmp/m5pahub-scan.bin ./examples/scan
	$(TINYGO) build -target=$(TARGET) -size short -o /tmp/m5pahub-validate.bin ./examples/validate

# Compile the public constructor for targets that are not ESP32. This is the
# agnosticism gate of docs/LIB-AGNOSTIC-AUDIT.md.
agnostic:
	@mkdir -p /tmp/pahub-agn && cd /tmp/pahub-agn && \
	  { printf 'module agn\n\ngo 1.26\n\nrequire %s v0.0.0\nreplace %s => %s\n' \
	      "$(shell awk '/^module /{print $$2}' go.mod)" \
	      "$(shell awk '/^module /{print $$2}' go.mod)" "$(CURDIR)" > go.mod; } && \
	  go mod tidy >/dev/null && \
	  printf 'package main\n\nimport lib "%s"\n\nvar _ = lib.New\n\nfunc main() {}\n' \
	      "$(shell awk '/^module /{print $$2}' go.mod)" > main.go && \
	  $(TINYGO) build -target=pico -o /tmp/pahub-agn/pico.bin . && \
	  $(TINYGO) build -target=arduino-nano33 -o /tmp/pahub-agn/nrf.bin . && \
	  echo "agnostic: pico + arduino-nano33 OK"

# Guided hardware test, one test per flash (see examples/validate/config.go).
validate:
	$(TINYGO) flash -target=$(TARGET) -port=$(PORT) -size short -monitor ./examples/validate

scan:
	$(TINYGO) flash -target=$(TARGET) -port=$(PORT) -size short -monitor ./examples/scan
