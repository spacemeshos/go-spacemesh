BINARY := go-spacemesh
VERSION := 0.0.1
COMMIT = $(shell git rev-parse HEAD)
BRANCH = $(shell git rev-parse --abbrev-ref HEAD)
BIN_DIR = $(shell pwd)/build
CURR_DIR = $(shell pwd)
CURR_DIR_WIN = $(shell cd)
export GO111MODULE = on

# Setup the -ldflags option to pass vars defined here to app vars
LDFLAGS = -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}"

PKGS = $(shell go list ./...)

PLATFORMS := windows linux darwin
os = $(word 1, $@)

all: install build
.PHONY: all

install:
ifeq ($(OS),Windows_NT) 
	setup_env.bat
else
	./setup_env.sh
endif
.PHONY: install

genproto:
ifeq ($(OS),Windows_NT) 
	scripts\win\genproto.bat
else
	./scripts/genproto.sh
endif
.PHONY: genproto

build:
ifeq ($(OS),Windows_NT)
	make genproto
	go build ${LDFLAGS} -o $(CURR_DIR_WIN)/$(BINARY).exe
else
	make genproto
	go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)
endif
.PHONY: build

tidy:
	go mod tidy
.PHONY: tidy

$(PLATFORMS):
ifeq ($(OS),Windows_NT)
	make genproto
	set GOOS=$(os)&&set GOARCH=amd64&&go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)
else
	make genproto
	GOOS=$(os) GOARCH=amd64 go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)
endif
.PHONY: $(PLATFORMS)

test:
	go test -short -p 1 ./...
.PHONY: test

test-tidy:
	# Working directory must be clean, or this test would be destructive
	git diff --quiet || (echo "\033[0;31mWorking directory not clean!\033[0m" && exit 1)
	# We expect `go mod tidy` not to change anything, the test should fail otherwise
	make tidy
	git diff --exit-code || (git checkout . && exit 1)
.PHONY: test-tidy

test-fmt:
	git diff --quiet || (echo "\033[0;31mWorking directory not clean!\033[0m" && exit 1)
	# We expect `go fmt` not to change anything, the test should fail otherwise
	go fmt ./...
	git diff --exit-code || (git checkout . && exit 1)
.PHONY: test-fmt

lint:
	./scripts/validate-lint.sh
.PHONY: lint

cover:
	@echo "mode: count" > cover-all.out
	@$(foreach pkg,$(PKGS),\
		go test -coverprofile=cover.out -covermode=count $(pkg);\
		tail -n +2 cover.out >> cover-all.out;)
	go tool cover -html=cover-all.out
.PHONY: cover
