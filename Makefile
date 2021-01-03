BINARY := go-spacemesh
INTERACTIVE := $(shell [ -t 0 ] && echo 1)
VERSION = $(shell cat version.txt)
COMMIT = $(shell git rev-parse HEAD)
SHA = $(shell git rev-parse --short HEAD)
CURR_DIR = $(shell pwd)
CURR_DIR_WIN = $(shell cd)
BIN_DIR = $(CURR_DIR)/build
BIN_DIR_WIN = $(CURR_DIR_WIN)/build
export GO111MODULE = on

# Read branch from git if running make manually
# Also allows BRANCH to be manually set
BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD)

# Setup the -ldflags option to pass vars defined here to app vars
LDFLAGS = -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}"

PKGS = $(shell go list ./...)

PLATFORMS := windows linux darwin freebsd
os = $(word 1, $@)

ifeq ($(BRANCH),develop)
        DOCKER_IMAGE_REPO := go-spacemesh
else
        DOCKER_IMAGE_REPO := go-spacemesh-dev
endif

# This prevents "the input device is not a TTY" error from docker in CI
DOCKERRUNARGS := --rm -e ES_PASSWD="$(ES_PASSWD)" \
	-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
	-e ES_USER=$(ES_USER) \
	-e ES_PASS=$(ES_PASS) \
	-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
	go-spacemesh-python:$(BRANCH)
ifdef INTERACTIVE
	DOCKERRUN := docker run -it $(DOCKERRUNARGS)
else
	DOCKERRUN := docker run -i $(DOCKERRUNARGS)
endif

all: install build
.PHONY: all


install:
ifeq ($(OS),Windows_NT) 
	setup_env.bat
else
	./setup_env.sh
endif
.PHONY: install


build:
ifeq ($(OS),Windows_NT)
	go build ${LDFLAGS} -o $(BIN_DIR_WIN)/$(BINARY).exe
else
	go build ${LDFLAGS} -o $(BIN_DIR)/$(BINARY)
endif
.PHONY: build


hare:
ifeq ($(OS),Windows_NT)
	cd cmd/hare ; go build -o $(BIN_DIR_WIN)/go-hare.exe
else
	cd cmd/hare ; go build -o $(BIN_DIR)/go-hare
endif
.PHONY: hare


p2p:
ifeq ($(OS),WINDOWS_NT)
	cd cmd/p2p ; go build -o $(BIN_DIR_WIN)/go-p2p.exe
else
	cd cmd/p2p ; go build -o $(BIN_DIR)/go-p2p
endif
.PHONY: p2p


sync:
ifeq ($(OS),WINDOWS_NT)
	cd cmd/sync ; go build -o $(BIN_DIR_WIN)/go-sync.exe
else
	cd cmd/sync ; go build -o $(BIN_DIR)/go-sync
endif
.PHONY: sync


harness:
ifeq ($(OS),WINDOWS_NT)
	cd cmd/integration ; go build -o $(BIN_DIR_WIN)/go-harness.exe
else
	cd cmd/integration ; go build -o $(BIN_DIR)/go-harness
endif
.PHONY: harness


tidy:
	go mod tidy
.PHONY: tidy


$(PLATFORMS):
ifeq ($(OS),Windows_NT)
	set GOOS=$(os)&&set GOARCH=amd64&&go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY).exe
else
	GOOS=$(os) GOARCH=amd64 go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)
endif
.PHONY: $(PLATFORMS)


docker-local-build:
	GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o $(BIN_DIR)/$(BINARY)
	cd cmd/hare ; GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/go-hare
	cd cmd/p2p ; GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/go-p2p
	cd cmd/sync ; GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/go-sync
	cd cmd/integration ; GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/go-harness
	cd build ; docker build -f ../DockerfilePrebuiltBinary -t $(DOCKER_IMAGE_REPO):$(BRANCH) .
.PHONY: docker-local-build


arm6:
	GOOS=linux GOARCH=arm GOARM=6 go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)
.PHONY: pi


test:
	ulimit -n 9999; go test -timeout 0 -p 1 ./...
.PHONY: test


test-no-app-test:
	ulimit -n 9999; go test -v -timeout 0 -p 1 -tags exclude_app_test ./...
.PHONY: test


test-only-app-test:
	ulimit -n 9999; go test -timeout 0 -p 1 -v -tags !exclude_app_test ./cmd/node
.PHONY: test


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
	@$(foreach pkg,$(PKGS),\
		go test -coverprofile=cover.out -covermode=count $(pkg);\
		tail -n +2 cover.out >> cover-all.out;)
	go tool cover -html=cover-all.out
.PHONY: cover


tag-and-build:
	git diff --quiet || (echo "\033[0;31mWorking directory not clean!\033[0m" && git --no-pager diff && exit 1)
	echo ${VERSION} > version.txt
	git commit -m "bump version to ${VERSION}" version.txt
	git tag ${VERSION}
	git push origin ${VERSION}
	docker build -t go-spacemesh:${VERSION} .
	docker tag go-spacemesh:${VERSION} spacemeshos/go-spacemesh:${VERSION}
	docker push spacemeshos/go-spacemesh:${VERSION}
.PHONY: tag-and-build


list-versions:
	@echo "Latest 5 tagged versions:\n"
	@git for-each-ref --sort=-creatordate --count=5 --format '%(creatordate:short): %(refname:short)' refs/tags
.PHONY: list-versions


dockerbuild-go:
	docker build -t $(DOCKER_IMAGE_REPO):$(BRANCH) .
.PHONY: dockerbuild-go


dockerbuild-test:
	docker build -f DockerFileTests --build-arg GCLOUD_KEY="$(GCLOUD_KEY)" \
	             --build-arg PROJECT_NAME="$(PROJECT_NAME)" \
	             --build-arg CLUSTER_NAME="$(CLUSTER_NAME)" \
	             --build-arg CLUSTER_ZONE="$(CLUSTER_ZONE)" \
	             -t go-spacemesh-python:$(BRANCH) .
.PHONY: dockerbuild-test


dockerbuild-test-elk:
	docker build -f DockerFileTests --build-arg GCLOUD_KEY="$(GCLOUD_KEY)" \
	             --build-arg PROJECT_NAME="$(PROJECT_NAME)" \
	             --build-arg CLUSTER_NAME="$(CLUSTER_NAME_ELK)" \
	             --build-arg CLUSTER_ZONE="$(CLUSTER_ZONE_ELK)" \
	             -t go-spacemesh-python:$(BRANCH) .
.PHONY: dockerbuild-test-elk


dockerpush: dockerbuild-go dockerpush-only
.PHONY: dockerpush

dockerpush-only:
	echo "$(DOCKER_PASSWORD)" | docker login -u "$(DOCKER_USERNAME)" --password-stdin
	docker tag $(DOCKER_IMAGE_REPO):$(BRANCH) spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)
	docker push spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)

ifeq ($(BRANCH),develop)
	docker tag $(DOCKER_IMAGE_REPO):$(BRANCH) spacemeshos/$(DOCKER_IMAGE_REPO):$(SHA)
	docker push spacemeshos/$(DOCKER_IMAGE_REPO):$(SHA)
endif
.PHONY: dockerpush-only


docker-local-push: docker-local-build dockerpush-only
.PHONY: docker-local-push


ifdef TEST
DELIM=::
endif


dockerrun-p2p-elk:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v p2p/test_p2p.py --tc-file=p2p/config.yaml --tc-format=yaml
.PHONY: dockerrun-p2p-elk

dockertest-p2p-elk: dockerbuild-test-elk dockerrun-p2p-elk
.PHONY: dockertest-p2p-elk


dockerrun-mining-elk:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v test_bs.py --tc-file=config.yaml --tc-format=yaml
.PHONY: dockerrun-mining-elk

dockertest-mining-elk: dockerbuild-test-elk dockerrun-mining-elk
.PHONY: dockertest-mining-elk


dockerrun-hare-elk:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif
	$(DOCKERRUN) pytest -s -v hare/test_hare.py::test_hare_sanity --tc-file=hare/config.yaml --tc-format=yaml
.PHONY: dockerrun-hare-elk


dockertest-hare-elk: dockerbuild-test-elk dockerrun-hare-elk
.PHONY: dockertest-hare-elk


dockerrun-sync-elk:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif

	$(DOCKERRUN) pytest -s -v sync/test_sync.py --tc-file=sync/config.yaml --tc-format=yaml

.PHONY: dockerrun-sync-elk

dockertest-sync-elk: dockerbuild-test-elk dockerrun-sync-elk
.PHONY: dockertest-sync-elk


dockerrun-late-nodes-elk:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif

	$(DOCKERRUN) pytest -s -v late_nodes/test_delayed.py --tc-file=late_nodes/delayed_config.yaml --tc-format=yaml

.PHONY: dockerrun-late-nodes-elk

dockertest-late-nodes-elk: dockerbuild-test-elk dockerrun-late-nodes-elk
.PHONY: dockertest-late-nodes-elk


dockerrun-genesis-voting-elk:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif

	$(DOCKERRUN) pytest -s -v sync/genesis/test_genesis_voting.py --tc-file=sync/genesis/config.yaml --tc-format=yaml

.PHONY: dockerrun-genesis-voting-elk

dockertest-genesis-voting-elk: dockerbuild-test-elk dockerrun-genesis-voting-elk
.PHONY: dockertest-genesis-voting-elk


dockerrun-blocks-add-node-elk:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif

	$(DOCKERRUN) pytest -s -v block_atx/add_node/test_blocks_add_node.py --tc-file=block_atx/add_node/config.yaml --tc-format=yaml

.PHONY: dockerrun-blocks-add-node-elk

dockertest-blocks-add-node-elk: dockerbuild-test-elk dockerrun-blocks-add-node-elk
.PHONY: dockertest-blocks-add-node-elk


dockerrun-blocks-remove-node-elk:
ifndef ES_PASS
	$(error ES_PASS is not set)
endif

	$(DOCKERRUN) pytest -s -v block_atx/remove_node/test_blocks_remove_node.py --tc-file=block_atx/remove_node/config.yaml --tc-format=yaml

.PHONY: dockerrun-blocks-remove-node-elk

dockertest-blocks-remove-node-elk: dockerbuild-test-elk dockerrun-blocks-remove-node-elk
.PHONY: dockertest-blocks-remove-node-elk


dockerrun-blocks-stress:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	$(DOCKERRUN) pytest -s -v stress/blocks_stress/test_stress_blocks.py --tc-file=stress/blocks_stress/config.yaml --tc-format=yaml

.PHONY: dockerrun-blocks-stress

dockertest-blocks-stress: dockerbuild-test dockerrun-blocks-stress
.PHONY: dockertest-blocks-stress


dockerrun-grpc-stress:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	$(DOCKERRUN) pytest -s -v stress/grpc_stress/test_stress_grpc.py --tc-file=stress/grpc_stress/config.yaml --tc-format=yaml

.PHONY: dockerrun-grpc-stress

dockertest-grpc-stress: dockerbuild-test dockerrun-grpc-stress
.PHONY: dockertest-grpc-stress


dockerrun-sync-stress:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	$(DOCKERRUN) pytest -s -v stress/sync_stress/test_sync.py --tc-file=stress/sync_stress/config.yaml --tc-format=yaml

.PHONY: dockerrun-sync-stress

dockertest-sync-stress: dockerbuild-test dockerrun-sync-stress
.PHONY: dockertest-sync-stress


dockerrun-tx-stress:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	$(DOCKERRUN) pytest -s -v stress/tx_stress/test_stress_txs.py --tc-file=stress/tx_stress/config.yaml --tc-format=yaml

.PHONY: dockerrun-tx-stress

dockertest-tx-stress: dockerbuild-test dockerrun-tx-stress
.PHONY: dockertest-tx-stress


# The following is used to run tests one after the other locally
dockerrun-test: dockerbuild-test-elk dockerrun-p2p-elk dockerrun-mining-elk dockerrun-hare-elk dockerrun-sync-elk dockerrun-late-nodes-elk dockerrun-blocks-add-node-elk dockerrun-blocks-remove-node-elk
.PHONY: dockerrun-test

dockerrun-all: dockerpush dockerrun-test
.PHONY: dockerrun-all

dockerrun-stress: dockerbuild-test dockerrun-blocks-stress dockerrun-grpc-stress dockerrun-sync-stress dockerrun-tx-stress
.PHONY: dockerrun-stress

dockertest-stress: dockerpush dockerrun-stress
.PHONY: dockertest-stress
