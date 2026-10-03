VERSION ?= dev
RELEASE_REPO ?= jo32/readyrig
UPDATE_FEED ?=
LDFLAGS = -X computer-use-server/internal/buildinfo.Version=$(VERSION) -X computer-use-server/internal/buildinfo.ReleaseRepo=$(RELEASE_REPO) -X computer-use-server/internal/buildinfo.UpdateFeed=$(UPDATE_FEED)

.PHONY: build cli test run web app release bench-check
build:
	MACOSX_DEPLOYMENT_TARGET=12.0 go build -ldflags '$(LDFLAGS)' -o bin/readyrig ./cmd/adapter
cli:
	go build -tags nogui -ldflags '$(LDFLAGS)' -o bin/readyrig ./cmd/adapter
	cp bin/readyrig bin/readyrig-web
test:
	node --test scripts/test-activity.cjs scripts/test-i18n.cjs scripts/test-export.cjs
	go test -race -tags nogui ./...
	python3 scripts/test-install.py
	python3 scripts/test-tui.py
run:
	go run ./cmd/adapter
web:
	go run -tags nogui ./cmd/adapter web
app: build
	MACOSX_DEPLOYMENT_TARGET=12.0 go build -tags nogui -ldflags '$(LDFLAGS)' -o bin/readyrig-cli ./cmd/adapter
	VERSION='$(VERSION)' PACKAGE_CLI_BINARY=bin/readyrig-cli sh scripts/package-macos.sh
release:
	VERSION='$(VERSION)' RELEASE_REPO='$(RELEASE_REPO)' sh scripts/release.sh
bench-check:
	go build -tags nogui -o bin/readyrig-bench ./cmd/adapter
	python3 scripts/bench-tools.py --check bin/readyrig-bench
