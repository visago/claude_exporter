BINARY  := claude_exporter
IMAGE   := visago/claude_exporter
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PLATFORMS := linux/amd64,linux/arm64

.PHONY: all fmt vet build docker docker-load clean

all: fmt vet build

fmt:
	go fmt ./...

vet:
	go vet ./...

build:
	go build -ldflags="-s -w" -o $(BINARY) .

# Build and push multi-arch image to registry.
docker:
	docker buildx build \
		--platform $(PLATFORMS) \
		--tag $(IMAGE):$(VERSION) \
		--tag $(IMAGE):latest \
		--push \
		.

# Build current platform only and load into local Docker daemon (for testing).
docker-load:
	docker buildx build \
		--load \
		--tag $(IMAGE):$(VERSION) \
		.

clean:
	rm -f $(BINARY)
