PREFIX ?= ~/.local

# The latest tag, plus commits-since and short SHA between releases, "-dirty"
# with uncommitted changes. Falls back to the bare SHA before the first tag.
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null)

# A repository with no commits yet describes as nothing at all.
ifeq ($(VERSION),)
	VERSION := dev
endif

LDFLAGS := -X main.version=$(VERSION)

# A dev install carries the race detector, so a daemon you run all day reports
# any race between the control socket and the HTTP handler instead of hiding it.
ifdef RACE
	BUILDFLAGS += -race
endif

build:
	go build $(BUILDFLAGS) -ldflags "$(LDFLAGS)" -o peek ./

install: build
	mkdir -p $(PREFIX)/bin
	cp -f peek $(PREFIX)/bin/peek
	# Signing keeps the binary's identity stable, so macOS does not ask again
	# for permission to accept incoming connections after every rebuild.
	command -v codesign >/dev/null 2>&1 && codesign --force --sign - $(PREFIX)/bin/peek || true

install-dev:
	$(MAKE) install RACE=1

test:
	go test ./...

test-race:
	go test -race -shuffle=on ./...

.PHONY: build install install-dev test test-race
