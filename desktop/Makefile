.PHONY: build build-windows test clean run install-autostart

BINARY_NAME=crosshare
GO=go

# Detect if go is in snap
ifeq ($(shell which go 2>/dev/null),)
    GO=/snap/bin/go
endif

build:
	$(GO) build -o $(BINARY_NAME) ./cmd/crosshare/

build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -ldflags "-H=windowsgui" -o $(BINARY_NAME).exe ./cmd/crosshare/

test:
	$(GO) test ./... -v -count=1

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME).exe

run: build
	./$(BINARY_NAME)

install-autostart:
	./scripts/install-systemd-user.sh

# Install dependencies
deps:
	$(GO) mod tidy

# Cross-compile for all platforms
release: clean
	# Linux amd64
	GOOS=linux GOARCH=amd64 $(GO) build -o $(BINARY_NAME)-linux-amd64 ./cmd/crosshare/
	# Windows amd64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -ldflags "-H=windowsgui" -o $(BINARY_NAME)-windows-amd64.exe ./cmd/crosshare/
