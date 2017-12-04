BINARY := go-unruly
VERSION := 0.0.1
COMMIT :=$(shell git rev-parse HEAD)
BRANCH :=$(shell git rev-parse --abbrev-ref HEAD)
BIN_DIR := $(shell pwd)/build
CURR_DIR :=$(shell pwd)

# Setup the -ldflags option for go build here, interpolate the variable values
LDFLAGS = -ldflags "-X app.Version=${VERSION} -X app.Commit=${COMMIT} -X app.Branch=${BRANCH}"

PKGS := $(shell go list ./... | grep -v /vendor)

PLATFORMS := windows linux darwin
os = $(word 1, $@)

build:
	go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)

.PHONY: $(PLATFORMS)

$(PLATFORMS):
	GOOS=$(os) GOARCH=amd64 go build ${LDFLAGS} -o $(BIN_DIR)/$(BINARY)-$(VERSION)-$(os)-amd64

.PHONY: build
