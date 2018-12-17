BINARY := go-spacemesh
VERSION := 0.0.1
COMMIT := $(shell git rev-parse HEAD)
BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
BIN_DIR := $(shell pwd)/build
CURR_DIR := $(shell pwd)

# Setup the -ldflags option to pass vars defined here to app vars
LDFLAGS := -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}"

PKGS := $(shell go list ./...)

PLATFORMS := windows linux darwin
os = $(word 1, $@)

all:
	make prepare
	make build

prepare:
	./setup_env.sh

genproto:
	./scripts/genproto.sh

build:
	make genproto
	go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)

$(PLATFORMS):
	make genproto
	GOOS=$(os) GOARCH=amd64 go build ${LDFLAGS} -o $(BIN_DIR)/$(BINARY)-$(VERSION)-$(os)-amd64

test:
	go test -p 1 ./...

.PHONY: all build test devtools cover $(PLATFORMS)

lint:
	./scripts/validate-lint.sh

cover:
	@echo "mode: count" > cover-all.out
	@$(foreach pkg,$(PKGS),\
		go test -coverprofile=cover.out -covermode=count $(pkg);\
		tail -n +2 cover.out >> cover-all.out;)
	go tool cover -html=cover-all.out
