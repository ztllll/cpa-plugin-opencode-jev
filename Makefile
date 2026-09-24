VERSION ?= 0.1.0
GO ?= go
PLUGIN_ID = opencode-jev
OUT = dist/opencode-jev-v$(VERSION).so

.PHONY: build test clean

build:
	test "$$($(GO) env GOOS)" = "linux"
	test "$$($(GO) env GOARCH)" = "amd64"
	mkdir -p "$(dir $(OUT))"
	CGO_ENABLED=1 $(GO) build -buildvcs=false -trimpath -buildmode=c-shared \
		-ldflags "-s -w -X main.pluginVersion=$(VERSION)" -o "$(OUT)" ./src
	rm -f dist/*.h

test:
	$(GO) vet ./src
	$(GO) test ./src

clean:
	rm -rf dist
