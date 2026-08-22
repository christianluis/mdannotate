BINARY  := mda
PREFIX  ?= /usr/local
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install uninstall test fmt vet clean dist run

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/mda

install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 0755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)
	@echo "mda liegt jetzt in $(DESTDIR)$(PREFIX)/bin"

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/$(BINARY)

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

run: build
	./$(BINARY) .

clean:
	rm -f $(BINARY)
	rm -rf dist

# dist baut die Binaries fuer eine Veroeffentlichung.
dist: clean
	@mkdir -p dist
	@for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do \
		os=$${target%/*}; arch=$${target#*/}; \
		echo "  $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BINARY)-$$os-$$arch ./cmd/mda; \
	done
