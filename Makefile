all: install build
.PHONY: all

LDFLAGS = -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}"
include Makefile-gpu.Inc
# TODO(nkryuchkov): uncomment when go-svm is imported
#include Makefile-svm.Inc

DOCKER_HUB ?= spacemeshos
TEST_LOG_LEVEL ?=

# Extra flags passed to `go test|build|run`
GOFLAGS ?=

COMMIT = $(shell git rev-parse HEAD)
SHA = $(shell git rev-parse --short HEAD)
BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD)

export GO111MODULE := on
export CGO_ENABLED := 1
export CGO_CFLAGS := "-DSQLITE_ENABLE_DBSTAT_VTAB=1"

PKGS = $(shell go list ./...)

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

DOCKER_IMAGE = $(DOCKER_IMAGE_REPO):$(BRANCH)

# We use a docker image corresponding to the commithash for staging and trying, to be safe
# filter here is used as a logical OR operation
ifeq ($(BRANCH),$(filter $(BRANCH),staging trying))
  DOCKER_IMAGE = $(DOCKER_IMAGE_REPO):$(SHA)
endif


install:
	go run scripts/check-go-version.go --major 1 --minor 18
	go mod download
	GO111MODULE=off go get golang.org/x/lint/golint
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s v1.48.0
	go install github.com/spacemeshos/go-scale/scalegen
	go install github.com/golang/mock/mockgen
	go install gotest.tools/gotestsum@v1.8.2
	go install honnef.co/go/tools/cmd/staticcheck@latest
.PHONY: install

build: go-spacemesh
.PHONY: build

# TODO(nkryuchkov): uncomment when go-svm is imported
get-libs: get-gpu-setup #get-svm
.PHONY: get-libs

gen-p2p-identity:
	cd $@ ; go build -o $(BIN_DIR)$@$(EXE) $(GOTAGS) $(GOFLAGS) .
hare p2p: get-libs
	cd $@ ; go build -o $(BIN_DIR)go-$@$(EXE) $(GOTAGS) $(GOFLAGS).
go-spacemesh: get-libs
	go build -o $(BIN_DIR)$@$(EXE) $(LDFLAGS) $(GOTAGS) $(GOFLAGS).
harness: get-libs
	cd cmd/integration ; go build -o $(BIN_DIR)go-$@$(EXE) $(GOTAGS) $(GOFLAGS) .
.PHONY: hare p2p harness go-spacemesh gen-p2p-identity

tidy:
	go mod tidy
.PHONY: tidy

ifeq ($(HOST_OS),$(filter $(HOST_OS),linux darwin))
windows:
	CC=x86_64-w64-mingw32-gcc $(MAKE) GOOS=$@ GOARCH=amd64 BIN_DIR=$(PROJ_DIR)build/ go-spacemesh
.PHONY: windows
arm6:
	$(error gpu lib is  not available for arm6 yet)
	#CC=x86_64-arm6-gcc $(MAKE) GOOS=$@ GOARCH=arm6 GOARM=6 BIN_DIR=$(PROJ_DIR)build/arm6/ go-spacemesh
.PHONY: arm6
$(HOST_OS): go-spacemesh
.PHONY: $(HOST_OS)
endif

ifeq ($(HOST_OS),windows)
windows: go-spacemesh
.PHONY: windows
endif

# available only for linux host because CGO usage
ifeq ($(HOST_OS),linux)
docker-local-build: go-spacemesh hare p2p harness
	cd build; docker build -f ../Dockerfile.prebuiltBinary -t $(DOCKER_IMAGE) .
.PHONY: docker-local-build
endif

# Clear tests cache
clear-test-cache:
	go clean -testcache

test: UNIT_TESTS = $(shell go list ./...  | grep -v systest)

test: get-libs
	$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" TEST_LOG_LEVEL=$(TEST_LOG_LEVEL) gotestsum -- -timeout 0 -p 1 $(GOFLAGS) $(UNIT_TESTS)
.PHONY: test

generate: get-libs
	$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" go generate ./...
.PHONY: generate

staticcheck: get-libs
	$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" staticcheck ./...
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

lint: golangci-lint
	# Golint is deprecated and frozen. Using golangci-lint instead.
	# golint --set_exit_status ./...
	go vet ./...
.PHONY: lint

golangci-lint:
	golangci-lint run --config .golangci.yml
.PHONY: golangci-lint

# Auto-fixes golangci-lint issues where possible.
golangci-lint-fix:
	golangci-lint run --config .golangci.yml --fix
.PHONY: golangci-lint-fix

golangci-lint-github-action:
	./bin/golangci-lint run --config .golangci.yml --out-format=github-actions
.PHONY: golangci-lint-github-action

cover:
	@echo "mode: count" > cover-all.out
	@export CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)";\
	  $(foreach pkg,$(PKGS),\
		go test -coverprofile=cover.out -covermode=count $(GOFLAGS) $(pkg);\
		tail -n +2 cover.out >> cover-all.out;)
	go tool cover -html=cover-all.out
.PHONY: cover

tag-and-build:
	git diff --quiet || (echo "\033[0;31mWorking directory not clean!\033[0m" && git --no-pager diff && exit 1)
	printf "${VERSION}" > version.txt
	git commit -m "bump version to ${VERSION}" version.txt
	git tag ${VERSION}
	git push origin ${VERSION}
	docker build -t go-spacemesh:${VERSION} .
	docker tag go-spacemesh:${VERSION} $(DOCKER_HUB)/go-spacemesh:${VERSION}
	docker push $(DOCKER_HUB)/go-spacemesh:${VERSION}
.PHONY: tag-and-build

list-versions:
	@echo "Latest 5 tagged versions:\n"
	@git for-each-ref --sort=-creatordate --count=5 --format '%(creatordate:short): %(refname:short)' refs/tags
.PHONY: list-versions

dockerbuild-go:
	docker build -t $(DOCKER_IMAGE) .
.PHONY: dockerbuild-go

dockerpush: dockerbuild-go dockerpush-only
.PHONY: dockerpush

dockerpush-only:
ifneq ($(DOCKER_USERNAME):$(DOCKER_PASSWORD),:)
	echo "$(DOCKER_PASSWORD)" | docker login -u "$(DOCKER_USERNAME)" --password-stdin
endif
	docker tag $(DOCKER_IMAGE) $(DOCKER_HUB)/$(DOCKER_IMAGE)
	docker push $(DOCKER_HUB)/$(DOCKER_IMAGE)

# for develop, we push an additional copy of the image using the commithash for archival
ifeq ($(BRANCH),develop)
	docker tag $(DOCKER_IMAGE) $(DOCKER_HUB)/$(DOCKER_IMAGE_REPO):$(SHA)
	docker push $(DOCKER_HUB)/$(DOCKER_IMAGE_REPO):$(SHA)
endif
.PHONY: dockerpush-only

docker-local-push: docker-local-build dockerpush-only
.PHONY: docker-local-push
