BINARY := ports
VERSION := $(shell cat VERSION 2>/dev/null || echo 1.0.0)
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: build install install-alias uninstall clean

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/ports/

install: build
	install -m 755 $(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed to /usr/local/bin/$(BINARY)"

install-alias: install
	ln -sf /usr/local/bin/$(BINARY) /usr/local/bin/whoisonport
	@echo "Alias installed: /usr/local/bin/whoisonport"

uninstall:
	rm -f /usr/local/bin/$(BINARY) /usr/local/bin/whoisonport

clean:
	rm -f $(BINARY)
