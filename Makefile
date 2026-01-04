## --- Versionierung (ENV hat Vorrang, sonst Fallbacks) ---
VERSION    ?= "0.1.0"
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE       ?= "2025-12-20"

IMAGE      ?= bodsch/server
NOCACHE    := --no-cache

# ---- config ----
BIN        := server
CMD        := ./cmd/server
GO         ?= go
GOFLAGS    ?= -trimpath -buildvcs=true
LDFLAGS := -s -w \
  -X 'main.version=$(VERSION)' \
  -X 'main.commit=$(COMMIT)' \
  -X 'main.date=$(DATE)'
TAGS       ?=

# Cross Build (anpassbar)
PLATFORMS  ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

# ---- helpers ----
define build_target
	@echo ">> build $(1)"
	@GOOS=$(word 1,$(subst /, ,$(1))) GOARCH=$(word 2,$(subst /, ,$(1))) \
		$(GO) build $(GOFLAGS) -tags '$(TAGS)' -ldflags '$(LDFLAGS)' -o dist/$(BIN)-$(subst /,-,$(1)) $(CMD)
endef

# ---- default ----
.PHONY: all
all: deps fmt vet build

.PHONY: deps
deps:
	$(GO) mod tidy
	$(GO) mod download all

# ---- quality ----
.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: test
test:
	$(GO) test ./...

.PHONY: race
race:
	$(GO) test ./... -race

# ---- build/run ----
.PHONY: build
build:
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o bin/$(BIN) $(CMD)

.PHONY: run
run: build
	./bin/$(BIN)

.PHONY: clean
clean:
	@rm -rf bin dist

.PHONY: print-version
print-version:
	@echo "version=$(VERSION) commit=$(COMMIT) date=$(DATE)"

.PHONY: container
container:
	docker buildx build ${NOCACHE} --platform linux/amd64 --tag ${IMAGE}:${VERSION} .

.PHONY: run-container
run-container:
	docker run -ti --rm -e DEBUG_ENTRYPOINT="true" ${IMAGE}:${VERSION} /bin/wait4x

# ---- release ----
.PHONY: release
release:
	@set -e
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
	  GOOS=$${p%/*}; GOARCH=$${p#*/}; \
	  echo ">> build $$GOOS/$$GOARCH"; \
	  BIN="dist/gallery_$${GOOS}_$${GOARCH}"; \
	  [ "$$GOOS" = "windows" ] && BIN="$${BIN}.exe"; \
	  CGO_ENABLED=0 GOOS=$$GOOS GOARCH=$$GOARCH \
	    go build -trimpath -buildvcs=true -ldflags '$(LDFLAGS)' \
	      -o "$$BIN" ./cmd/gallery; \
	done

# ---- help ----
.PHONY: help
help:
	@echo "Targets:"
	@echo "  make build           # baut ./bin/$(BIN)"
	@echo "  make run             # startet mit -config $(CONFIG)"
	@echo "  make test            # Unit-Tests"
	@echo "  make release         # Cross-Build in ./dist"
	@echo "  make fmt vet tidy    # Pflege"
	@echo "  make clean           # räumt auf"
