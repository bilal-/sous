MODULE   := github.com/bilal-/sous
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(MODULE)/internal/cli.Version=$(VERSION)

.PHONY: build test cover lint ci install release-dry clean tap

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/sous ./cmd/sous

test:
	go vet ./... && go test -race ./...

cover:
	go test -coverpkg=./... -coverprofile=coverage.out ./... >/dev/null
	go tool cover -func=coverage.out | tail -1

lint:
	@test -z "$$(gofmt -l .)" || { echo "gofmt:"; gofmt -l .; exit 1; }
	go vet ./...

ci: lint test

install: build
	mkdir -p $(HOME)/.local/bin
	ln -sf $(CURDIR)/bin/sous $(HOME)/.local/bin/sous

# Requires goreleaser (brew install goreleaser). Builds every target locally
# into dist/ without publishing.
release-dry:
	goreleaser release --snapshot --clean

clean:
	rm -rf bin dist coverage.out

# After a release is published: update the Homebrew formula in
# bilal-/homebrew-tap. Usage: make tap VERSION=v0.1.1
tap:
	@test -n "$(VERSION)" || { echo "usage: make tap VERSION=vX.Y.Z"; exit 1; }
	rm -rf .tap && gh repo clone bilal-/homebrew-tap .tap -- -q
	scripts/homebrew-formula.sh $(VERSION) > .tap/Formula/sous.rb
	cd .tap && git add Formula/sous.rb && git commit -qm "sous $(VERSION:v%=%)" && git push -q
	rm -rf .tap
