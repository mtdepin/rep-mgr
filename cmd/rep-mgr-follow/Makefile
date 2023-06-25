# go source files
SRC := $(shell find ../.. -type f -name '*.go')
GOPATH := $(shell go env GOPATH)
GOFLAGS := "-trimpath"

all: rep-mgr-follow

rep-mgr-follow: $(SRC)
	go build $(GOFLAGS) -mod=readonly -ldflags "-X main.commit=$(shell git rev-parse HEAD)"

build: rep-mgr-follow

install:
	go install $(GOFLAGS) -ldflags "-X main.commit=$(shell git rev-parse HEAD)"

clean:
	rm -f rep-mgr-follow

.PHONY: clean install build
