ifeq ($(OS),Windows_NT)
	BINARY := go-spacemesh.exe
else
	BINARY := go-spacemesh
endif
VERSION := 0.0.1
COMMIT = $(shell git rev-parse HEAD)
BRANCH = $(shell git rev-parse --abbrev-ref HEAD)
BIN_DIR = $(shell pwd)/build
CURR_DIR = $(shell pwd)
export GO111MODULE = on

# Setup the -ldflags option to pass vars defined here to app vars
LDFLAGS = -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}"

PKGS = $(shell go list ./...)

PLATFORMS := windows linux darwin
os = $(word 1, $@)

all: install build
.PHONY: all

install:
	"${CURR_DIR}/setup_env.sh"
.PHONY: install

genproto:
	"${CURR_DIR}/scripts/genproto.sh"
.PHONY: genproto

build:
	make genproto
	go build ${LDFLAGS} -o "${CURR_DIR}/$(BINARY)"
.PHONY: build

tidy:
	go mod tidy
.PHONY: tidy

$(PLATFORMS):
	make genproto
	GOOS=$(os) GOARCH=amd64 go build ${LDFLAGS} -o $(BIN_DIR)/$(BINARY)-$(VERSION)-$(os)-amd64
.PHONY: $(PLATFORMS)

test:
	go test -p 1 ./...
.PHONY: test

test-tidy:
	# We expect `go mod tidy` not to change anything, the test should fail otherwise
	make tidy
	git diff --exit-code
.PHONY: test-tidy

lint:
	"${CURR_DIR}/scripts/validate-lint.sh"
.PHONY: lint

cover:
	@echo "mode: count" > cover-all.out
	@$(foreach pkg,$(PKGS),\
		go test -coverprofile=cover.out -covermode=count $(pkg);\
		tail -n +2 cover.out >> cover-all.out;)
	go tool cover -html=cover-all.out
.PHONY: cover
