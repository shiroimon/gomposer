VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.Version=$(VERSION)"

.PHONY: build clean install

build:
	go build $(LDFLAGS) -o bin/gomposer ./cmd

clean:
	rm -rf bin/

install:
	go install $(LDFLAGS) ./cmd
