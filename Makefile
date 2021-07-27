all: install build
.PHONY: all

LDFLAGS = -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}"
include Makefile.Inc

DOCKER_HUB ?= spacemeshos

COMMIT = $(shell git rev-parse HEAD)
SHA = $(shell git rev-parse --short HEAD)
BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD)

export GO111MODULE := on
export CGO_ENABLED := 1
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


# This prevents "the input device is not a TTY" error from docker in CI
DOCKERRUNARGS := --rm \
	-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
 	-e CLUSTER_NAME=$(CLUSTER_NAME) \
	-e CLUSTER_ZONE=$(CLUSTER_ZONE) \
	-e PROJECT_NAME=$(PROJECT_NAME) \
	-e ES_USER=$(ES_USER) \
	-e ES_PASS=$(ES_PASS) \
	-e ES_PASSWD=$(ES_PASSWD) \
	-e MAIN_ES_IP=$(MAIN_ES_IP) \
	-e TD_QUEUE_NAME=$(TD_QUEUE_NAME) \
	-e TD_QUEUE_ZONE=$(TD_QUEUE_ZONE) \
	-e DUMP_QUEUE_NAME=$(DUMP_QUEUE_NAME) \
	-e DUMP_QUEUE_ZONE=$(DUMP_QUEUE_ZONE)

DOCKER_IMAGE = $(DOCKER_IMAGE_REPO):$(BRANCH)

# We use a docker image corresponding to the commithash for staging and trying, to be safe
# filter here is used as a logical OR operation
ifeq ($(BRANCH),$(filter $(BRANCH),staging trying))
  DOCKER_IMAGE = $(DOCKER_IMAGE_REPO):$(SHA)
endif

DOCKERRUNARGS := $(DOCKERRUNARGS) -e CLIENT_DOCKER_IMAGE=$(DOCKER_HUB)/$(DOCKER_IMAGE)

ifdef INTERACTIVE
  DOCKERRUN := docker run -it $(DOCKERRUNARGS) go-spacemesh-python:$(BRANCH)
else
  DOCKERRUN := docker run -i $(DOCKERRUNARGS) go-spacemesh-python:$(BRANCH)
endif

install:
	go run scripts/check-go-version.go --major 1 --minor 15
	go mod download
	GO111MODULE=off go get golang.org/x/lint/golint
.PHONY: install

build: go-spacemesh
.PHONY: build

hare p2p sync: get-gpu-setup
	cd cmd/$@ ; go build -o $(BIN_DIR)go-$@$(EXE) $(GOTAGS) .
go-spacemesh: get-gpu-setup
	go build -o $(BIN_DIR)$@$(EXE) $(LDFLAGS) $(GOTAGS) .
harness: get-gpu-setup
	cd cmd/integration ; go build -o $(BIN_DIR)go-$@$(EXE) $(GOTAGS) .
.PHONY: hare p2p sync harness go-spacemesh

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
docker-local-build: go-spacemesh hare p2p sync harness
	cd build; docker build -f ../DockerfilePrebuiltBinary -t $(DOCKER_IMAGE) .
.PHONY: docker-local-build
endif

test test-all: get-gpu-setup
	$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" go test -v -timeout 0 -p 1 ./...
.PHONY: test

test-no-app-test: get-gpu-setup
	$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)"  go test -v -timeout 0 -p 1 -tags exclude_app_test ./...
.PHONY: test-no-app-test

test-only-app-test: get-gpu-setup
	$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" go test -timeout 0 -p 1 -v -tags !exclude_app_test ./cmd/node
.PHONY: test-only-app-test

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

lint:
	golint --set_exit_status ./...
	go vet ./...
.PHONY: lint

cover:
	@echo "mode: count" > cover-all.out
	@export CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)";\
	  $(foreach pkg,$(PKGS),\
		go test -coverprofile=cover.out -covermode=count $(pkg);\
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

dockerbuild-test:
	docker build -f DockerFileTests \
	             --build-arg GCLOUD_KEY="$(GCLOUD_KEY)" \
	             --build-arg PROJECT_NAME="$(PROJECT_NAME)" \
	             --build-arg CLUSTER_NAME="$(CLUSTER_NAME)" \
	             --build-arg CLUSTER_ZONE="$(CLUSTER_ZONE)" \
	             -t go-spacemesh-python:$(BRANCH) .
.PHONY: dockerbuild-test

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

ifdef TEST
DELIM=::
endif

dockerrun-p2p:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v p2p/test_p2p.py --tc-file=p2p/config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-p2p

dockertest-p2p: dockerbuild-test dockerrun-p2p
.PHONY: dockertest-p2p

dockerrun-mining:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v test_bs.py --tc-file=config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-mining

dockertest-mining: dockerbuild-test dockerrun-mining
.PHONY: dockertest-mining

dockerrun-hare:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v hare/test_hare.py::test_hare_sanity --tc-file=hare/config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-hare

dockertest-hare: dockerbuild-test dockerrun-hare
.PHONY: dockertest-hare

dockerrun-sync:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v sync/test_sync.py --tc-file=sync/config.yaml --tc-format=yaml $(EXTRA_PARAMS)

.PHONY: dockerrun-sync

dockertest-sync: dockerbuild-test dockerrun-sync
.PHONY: dockertest-sync

dockerrun-late-nodes:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v late_nodes/test_delayed.py --tc-file=late_nodes/delayed_config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-late-nodes

dockertest-late-nodes: dockerbuild-test dockerrun-late-nodes
.PHONY: dockertest-late-nodes

dockerrun-genesis-voting:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v sync/genesis/test_genesis_voting.py --tc-file=sync/genesis/config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-genesis-voting

dockertest-genesis-voting: dockerbuild-test dockerrun-genesis-voting
.PHONY: dockertest-genesis-voting

dockerrun-blocks-add-node:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v block_atx/add_node/test_blocks_add_node.py --tc-file=block_atx/add_node/config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-blocks-add-node

dockertest-blocks-add-node: dockerbuild-test dockerrun-blocks-add-node
.PHONY: dockertest-blocks-add-node

dockerrun-blocks-remove-node:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v block_atx/remove_node/test_blocks_remove_node.py --tc-file=block_atx/remove_node/config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-blocks-remove-node

dockertest-blocks-remove-node: dockerbuild-test dockerrun-blocks-remove-node
.PHONY: dockertest-blocks-remove-node

dockerrun-tortoise-beacon:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v tortoise_beacon/test_tortoise_beacon.py --tc-file=tortoise_beacon/config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-tortoise-beacon

dockertest-tortoise-beacon: dockerbuild-test dockerrun-tortoise-beacon
.PHONY: dockertest-tortoise-beacon

dockerrun-blocks-stress:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v stress/blocks_stress/test_stress_blocks.py --tc-file=stress/blocks_stress/config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-blocks-stress

dockertest-blocks-stress: dockerbuild-test dockerrun-blocks-stress
.PHONY: dockertest-blocks-stress

dockerrun-grpc-stress:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v stress/grpc_stress/test_stress_grpc.py --tc-file=stress/grpc_stress/config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-grpc-stress

dockertest-grpc-stress: dockerbuild-test dockerrun-grpc-stress
.PHONY: dockertest-grpc-stress

dockerrun-sync-stress:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v stress/sync_stress/test_sync.py --tc-file=stress/sync_stress/config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-sync-stress

dockertest-sync-stress: dockerbuild-test dockerrun-sync-stress
.PHONY: dockertest-sync-stress

dockerrun-tx-stress:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v stress/tx_stress/test_stress_txs.py --tc-file=stress/tx_stress/config.yaml --tc-format=yaml $(EXTRA_PARAMS)
.PHONY: dockerrun-tx-stress

dockertest-tx-stress: dockerbuild-test dockerrun-tx-stress
.PHONY: dockertest-tx-stress

# The following is used to run tests one after the other locally
dockerrun-test: dockerbuild-test dockerrun-p2p dockerrun-mining dockerrun-hare dockerrun-sync dockerrun-late-nodes dockerrun-blocks-add-node dockerrun-blocks-remove-node dockerrun-tortoise-beacon
.PHONY: dockerrun-test

dockerrun-all: dockerpush dockerrun-test
.PHONY: dockerrun-all

dockerrun-stress: dockerbuild-test dockerrun-blocks-stress dockerrun-grpc-stress dockerrun-sync-stress dockerrun-tx-stress
.PHONY: dockerrun-stress

dockertest-stress: dockerpush dockerrun-stress
.PHONY: dockertest-stress
