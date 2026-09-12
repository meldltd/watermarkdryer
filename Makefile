GO ?= go
BINARY := bin/watermarkdryer

.PHONY: build test check release

build:
	@mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o $(BINARY) .

test:
	$(GO) test ./...

check:
	$(GO) vet ./...
	$(GO) test -race ./...

release:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags="-s -w" -o dist/watermarkdryer-darwin-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -ldflags="-s -w" -o dist/watermarkdryer-darwin-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags="-s -w" -o dist/watermarkdryer-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="-s -w" -o dist/watermarkdryer-linux-arm64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags="-s -w" -o dist/watermarkdryer-windows-amd64.exe .
