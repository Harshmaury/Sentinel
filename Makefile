# Makefile — Sentinel
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X main.sentinelVersion=$(VERSION)
BINDIR  := $(HOME)/bin

.PHONY: all build clean

all: build

build:
	@echo "  → sentinel $(VERSION)"
	@CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINDIR)/sentinel ./cmd/sentinel/

clean:
	@rm -f $(BINDIR)/sentinel
