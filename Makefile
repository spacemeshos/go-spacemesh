VERSION ?= $(shell git describe --tags)
COMMIT = $(shell git rev-parse HEAD)
BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD)

GOLANGCI_LINT_VERSION := v1.57.0
STATICCHECK_VERSION := v0.4.7
GOTESTSUM_VERSION := v1.11.0
GOSCALE_VERSION := v1.2.0
MOCKGEN_VERSION := v0.4.0

# Add an indicator to the branch name if dirty and use commithash if running in detached mode
ifeq ($(BRANCH),HEAD)
    BRANCH = $(SHA)
endif
$(shell git diff --quiet)
ifneq ($(.SHELLSTATUS),0)
	BRANCH := $(BRANCH)-dirty
endif

SHA = $(shell git rev-parse --short HEAD)
DOCKER_HUB ?= spacemeshos
DOCKER_IMAGE_REPO ?= go-spacemesh-dev
DOCKER_IMAGE_VERSION ?= $(SHA)

C_LDFLAGS = -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}
ifneq (,$(findstring nomain,$(VERSION)))
    C_LDFLAGS += -X main.noMainNet=true
endif
LDFLAGS = -ldflags "$(C_LDFLAGS)"

include Makefile-libs.Inc

UNIT_TESTS ?= $(shell go list ./...  | grep -v systest/tests | grep -v genvm/cmd)

export CGO_ENABLED := 1
export CGO_CFLAGS := $(CGO_CFLAGS) -DSQLITE_ENABLE_DBSTAT_VTAB=1

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
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s $(GOLANGCI_LINT_VERSION)
	go install github.com/spacemeshos/go-scale/scalegen@$(GOSCALE_VERSION)
	go install go.uber.org/mock/mockgen@$(MOCKGEN_VERSION)
	go install gotest.tools/gotestsum@$(GOTESTSUM_VERSION)
	go install honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
.PHONY: install

build: go-spacemesh get-profiler get-postrs-service
.PHONY: build

get-libs: get-postrs-lib get-postrs-service

get-profiler: get-postrs-profiler

merge-nodes: get-libs
	cd cmd/merge-nodes ; go build -o $(BIN_DIR)$@$(EXE) -ldflags "-X main.version=${VERSION}" .
.PHONY: merge-nodes

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

list-versions:
	@echo "Latest 5 tagged versions:\n"
	@git for-each-ref --sort=-creatordate --count=5 --format '%(creatordate:short): %(refname:short)' refs/tags
.PHONY: list-versions

dockerbuild-go:
	DOCKER_BUILDKIT=1 docker build \
		--secret id=mynetrc,src=$(HOME)/.netrc \
		--build-arg VERSION=${VERSION} \
		-t go-spacemesh:$(SHA) \
		-t $(DOCKER_HUB)/$(DOCKER_IMAGE_REPO):$(DOCKER_IMAGE_VERSION) \
		.
.PHONY: dockerbuild-go

dockerpush: dockerbuild-go dockerpush-only
.PHONY: dockerpush

dockerpush-only:
ifneq ($(DOCKER_USERNAME):$(DOCKER_PASSWORD),:)
	echo "$(DOCKER_PASSWORD)" | docker login -u "$(DOCKER_USERNAME)" --password-stdin
endif
	docker tag go-spacemesh:$(SHA) $(DOCKER_HUB)/$(DOCKER_IMAGE_REPO):$(DOCKER_IMAGE_VERSION)
	docker push $(DOCKER_HUB)/$(DOCKER_IMAGE_REPO):$(DOCKER_IMAGE_VERSION)
.PHONY: dockerpush-only

dockerbuild-bs:
	DOCKER_BUILDKIT=1 docker build \
		--secret id=mynetrc,src=$(HOME)/.netrc \
		-t go-spacemesh-bs:$(SHA) \
		-t $(DOCKER_HUB)/$(DOCKER_IMAGE_REPO)-bs:$(DOCKER_IMAGE_VERSION) \
		-f ./bootstrap.Dockerfile \
		.
.PHONY: dockerbuild-bs

dockerpush-bs: dockerbuild-bs dockerpush-bs-only
.PHONY: dockerpush-bs

dockerpush-bs-only:
ifneq ($(DOCKER_USERNAME):$(DOCKER_PASSWORD),:)
	echo "$(DOCKER_PASSWORD)" | docker login -u "$(DOCKER_USERNAME)" --password-stdin
endif
	docker tag go-spacemesh-bs:$(SHA) $(DOCKER_HUB)/$(DOCKER_IMAGE_REPO)-bs:$(DOCKER_IMAGE_VERSION)
	docker push $(DOCKER_HUB)/$(DOCKER_IMAGE_REPO)-bs:$(DOCKER_IMAGE_VERSION)
.PHONY: dockerpush-bs-only

fuzz:
	@$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" ./scripts/fuzz.sh $(FUZZTIME)
.PHONY: fuzz
