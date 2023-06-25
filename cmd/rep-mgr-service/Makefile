# go source files
SRC := $(shell find ../.. -type f -name '*.go')
GOPATH := $(shell go env GOPATH)
GOFLAGS := "-trimpath"

all: rep-mgr-service

rep-mgr-service: $(SRC)
	go build $(GOFLAGS) -mod=readonly -ldflags "-X main.commit=$(shell git rev-parse HEAD)"

build: rep-mgr-service

install:
	go install $(GOFLAGS) -ldflags "-X main.commit=$(shell git rev-parse HEAD)"

clean:
	rm -f rep-mgr-service

.PHONY: clean install build
