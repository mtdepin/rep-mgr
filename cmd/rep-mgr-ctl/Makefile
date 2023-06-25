# go source files
SRC := $(shell find ../.. -type f -name '*.go')
GOPATH := $(shell go env GOPATH)
GOFLAGS := "-trimpath"

all: rep-mgr-ctl

rep-mgr-ctl: $(SRC)
	go build $(GOFLAGS) -mod=readonly

build: rep-mgr-ctl

install:
	go install $(GOFLAGS)

clean:
	rm -f rep-mgr-ctl

.PHONY: clean install build
