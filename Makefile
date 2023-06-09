LDFLAGS = -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}"
include Makefile-libs.Inc

DOCKER_HUB ?= spacemeshos
UNIT_TESTS ?= $(shell go list ./...  | grep -v systest/tests | grep -v cmd/node | grep -v cmd/gen-p2p-identity | grep -v cmd/trace | grep -v genvm/cmd)

COMMIT = $(shell git rev-parse HEAD)
SHA = $(shell git rev-parse --short HEAD)
BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD)

export CGO_ENABLED := 1
export CGO_CFLAGS := $(CGO_CFLAGS) -DSQLITE_ENABLE_DBSTAT_VTAB=1

# These commands cause problems on Windows
ifeq ($(OS),Windows_NT)
  # Just assume we're in interactive mode on Windows
  INTERACTIVE = 1
  VERSION ?= $(shell type version.txt)
else
  INTERACTIVE := $(shell [ -t 0 ] && echo 1)
  VERSION ?= $(shell cat version.txt)
endif

# Add an indicator to the branch name if dirty and use commithash if running in detached mode
ifeq ($(BRANCH),HEAD)
    BRANCH = $(SHA)
endif
$(shell git diff --quiet)
ifneq ($(.SHELLSTATUS),0)
	BRANCH := $(BRANCH)-dirty
endif

ifeq ($(BRANCH),develop)
  DOCKER_IMAGE_REPO := go-spacemesh
else
  DOCKER_IMAGE_REPO := go-spacemesh-dev
endif

DOCKER_IMAGE = $(DOCKER_IMAGE_REPO):$(SHA)
DOCKER_BS_IMAGE = $(DOCKER_IMAGE_REPO)-bs:$(SHA)

# setting extra command line params for the CI tests pytest commands
ifdef namespace
    EXTRA_PARAMS:=$(EXTRA_PARAMS) --namespace=$(namespace)
endif

ifdef delns
    EXTRA_PARAMS:=$(EXTRA_PARAMS) --delns=$(delns)
endif

ifdef dump
    EXTRA_PARAMS:=$(EXTRA_PARAMS) --dump=$(dump)
endif

FUZZTIME ?= "10s"

all: install build
.PHONY: all

install:
	git lfs install
	go mod download
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v1.52.0
	go install github.com/spacemeshos/go-scale/scalegen@v1.1.9
	go install github.com/golang/mock/mockgen
	go install gotest.tools/gotestsum@v1.9.0
	go install honnef.co/go/tools/cmd/staticcheck@v0.3.3
.PHONY: install

build: go-spacemesh get-profiler
.PHONY: build

get-libs: get-postrs-lib
.PHONY: get-libs

get-profiler: get-postrs-profiler
.PHONY: get-profiler

gen-p2p-identity:
	cd cmd/gen-p2p-identity ; go build -o $(BIN_DIR)$@$(EXE) .
.PHONY: gen-p2p-identity

go-spacemesh: get-libs
	cd cmd/node ; go build -o $(BIN_DIR)$@$(EXE) $(LDFLAGS) .
.PHONY: go-spacemesh gen-p2p-identity

bootstrapper:
	cd cmd/bootstrapper ;  go build -o $(BIN_DIR)go-$@$(EXE) .
.PHONY: bootstrapper

tidy:
	go mod tidy
.PHONY: tidy

# Clear tests cache
clear-test-cache:
	go clean -testcache
.PHONY: clear-test-cache

test: get-libs
	@$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" gotestsum -- -race -timeout 5m -p 1 $(UNIT_TESTS)
.PHONY: test

generate: get-libs
	@$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" go generate ./...
.PHONY: generate

test-generate:
	# Working directory must be clean, or this test would be destructive
	@git diff --quiet || (echo "\033[0;31mWorking directory not clean!\033[0m" && git --no-pager diff && exit 1)
	# We expect `go generate` not to change anything, the test should fail otherwise
	@make generate
	@git diff --name-only --diff-filter=AM --exit-code . || { echo "\nPlease rerun 'make generate' and commit changes.\n"; exit 1; }
.PHONY: test-generate

staticcheck: get-libs
	@$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" staticcheck ./...
.PHONY: staticcheck

test-tidy:
	# Working directory must be clean, or this test would be destructive
	git diff --quiet || (echo "\033[0;31mWorking directory not clean!\033[0m" && git --no-pager diff && exit 1)
	# We expect `go mod tidy` not to change anything, the test should fail otherwise
	make tidy
	git diff --exit-code || (git --no-pager diff && git checkout . && exit 1)
.PHONY: test-tidy

test-fmt:
	git diff --quiet || (echo "\033[0;31mWorking directory not clean!\033[0m" && git --no-pager diff && exit 1)
	# We expect `go fmt` not to change anything, the test should fail otherwise
	go fmt ./...
	git diff --exit-code || (git --no-pager diff && git checkout . && exit 1)
.PHONY: test-fmt

lint: get-libs
	./bin/golangci-lint run --config .golangci.yml
.PHONY: lint

# Auto-fixes golangci-lint issues where possible.
lint-fix: get-libs
	./bin/golangci-lint run --config .golangci.yml --fix
.PHONY: lint-fix

lint-github-action: get-libs
	./bin/golangci-lint run --config .golangci.yml --out-format=github-actions
.PHONY: lint-github-action

cover: get-libs
	@$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" go test -coverprofile=cover.out -timeout 0 -p 1 -coverpkg=./... $(UNIT_TESTS)
.PHONY: cover

tag-and-build:
	git diff --quiet || (echo "\033[0;31mWorking directory not clean!\033[0m" && git --no-pager diff && exit 1)
	printf "${VERSION}" > version.txt
	git commit -m "bump version to ${VERSION}" version.txt
	git tag ${VERSION}
	git push origin ${VERSION}
	DOCKER_BUILDKIT=1 docker build -t go-spacemesh:${VERSION} .
	docker tag go-spacemesh:${VERSION} $(DOCKER_HUB)/go-spacemesh:${VERSION}
	docker push $(DOCKER_HUB)/go-spacemesh:${VERSION}
.PHONY: tag-and-build

list-versions:
	@echo "Latest 5 tagged versions:\n"
	@git for-each-ref --sort=-creatordate --count=5 --format '%(creatordate:short): %(refname:short)' refs/tags
.PHONY: list-versions

dockerbuild-go:
	DOCKER_BUILDKIT=1 docker build -t $(DOCKER_IMAGE) .
.PHONY: dockerbuild-go

dockerpush: dockerbuild-go dockerpush-only
.PHONY: dockerpush

dockerpush-only:
ifneq ($(DOCKER_USERNAME):$(DOCKER_PASSWORD),:)
	echo "$(DOCKER_PASSWORD)" | docker login -u "$(DOCKER_USERNAME)" --password-stdin
endif
	docker tag $(DOCKER_IMAGE) $(DOCKER_HUB)/$(DOCKER_IMAGE)
	docker push $(DOCKER_HUB)/$(DOCKER_IMAGE)
.PHONY: dockerpush-only

dockerbuild-bs:
	DOCKER_BUILDKIT=1 docker build -t $(DOCKER_BS_IMAGE) -f ./bootstrap.Dockerfile .
.PHONY: dockerbuild-bs

dockerpush-bs: dockerbuild-bs dockerpush-bs-only
.PHONY: dockerpush-bs

dockerpush-bs-only:
ifneq ($(DOCKER_USERNAME):$(DOCKER_PASSWORD),:)
	echo "$(DOCKER_PASSWORD)" | docker login -u "$(DOCKER_USERNAME)" --password-stdin
endif
	docker tag $(DOCKER_BS_IMAGE) $(DOCKER_HUB)/$(DOCKER_BS_IMAGE)
	docker push $(DOCKER_HUB)/$(DOCKER_BS_IMAGE)
.PHONY: dockerpush-bs-only

fuzz:
	@$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" ./scripts/fuzz.sh $(FUZZTIME)
.PHONY: fuzz
