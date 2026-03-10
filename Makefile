.PHONY: build install clean vet

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build:
	go build -ldflags "-X github.com/doki-stack/infrastructure/internal/cli.Version=$(VERSION)" -o dokictl ./cmd/dokictl/

install:
	go install -ldflags "-X github.com/doki-stack/infrastructure/internal/cli.Version=$(VERSION)" ./cmd/dokictl/

vet:
	go vet ./...

clean:
	rm -f dokictl
