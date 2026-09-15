BIN     := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.Version=$(VERSION)

.PHONY: build vet test fmt lint image
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN)/landscape ./cmd/landscape

vet:
	go vet ./...

test:
	go test ./...

fmt:
	gofmt -w .

lint: vet
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt:"; echo "$$out"; exit 1; fi

# Deployment is GitOps: a vX.Y.Z tag builds the image to GHCR (.github/workflows/
# deploy.yml) and Flux deploys it (sujaykumarsuman/infra). `make` only builds.
image:
	docker build -t landscape:$(VERSION) .
