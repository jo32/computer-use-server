VERSION ?= dev
RELEASE_REPO ?= jo32/computer-use-server
UPDATE_FEED ?=
LDFLAGS = -X computer-use-server/internal/buildinfo.Version=$(VERSION) -X computer-use-server/internal/buildinfo.ReleaseRepo=$(RELEASE_REPO) -X computer-use-server/internal/buildinfo.UpdateFeed=$(UPDATE_FEED)

.PHONY: build cli test run web app release
build:
	MACOSX_DEPLOYMENT_TARGET=12.0 go build -ldflags '$(LDFLAGS)' -o bin/relay ./cmd/adapter
cli:
	go build -tags nogui -ldflags '$(LDFLAGS)' -o bin/relay-web ./cmd/adapter
test:
	go test -race -tags nogui ./...
run:
	go run ./cmd/adapter
web:
	go run -tags nogui ./cmd/adapter web
app: build
	VERSION='$(VERSION)' sh scripts/package-macos.sh
release:
	VERSION='$(VERSION)' RELEASE_REPO='$(RELEASE_REPO)' sh scripts/release.sh
