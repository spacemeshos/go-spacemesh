BINARY := go-spacemesh
VERSION = $(shell cat version.txt)
COMMIT = $(shell git rev-parse HEAD)
SHA = $(shell git rev-parse --short HEAD)
CURR_DIR = $(shell pwd)
CURR_DIR_WIN = $(shell cd)
BIN_DIR = $(CURR_DIR)/build
BIN_DIR_WIN = $(CURR_DIR_WIN)/build
export GO111MODULE = on

BRANCH := $(shell bash -c 'if [ "$$TRAVIS_PULL_REQUEST" == "false" ]; then echo $$TRAVIS_BRANCH; else echo $$TRAVIS_PULL_REQUEST_BRANCH; fi')

# Set BRANCH when running make manually
ifeq ($(BRANCH),)
BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
endif

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


build: genproto
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


$(PLATFORMS): genproto
ifeq ($(OS),Windows_NT)
	set GOOS=$(os)&&set GOARCH=amd64&&go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY).exe
else
	GOOS=$(os) GOARCH=amd64 go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)
endif
.PHONY: $(PLATFORMS)


docker-local-build: genproto
	GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o $(BIN_DIR)/$(BINARY)
	cd cmd/hare ; GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/go-hare
	cd cmd/p2p ; GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/go-p2p
	cd cmd/sync ; GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/go-sync
	cd cmd/integration ; GOOS=linux GOARCH=amd64 go build -o $(BIN_DIR)/go-harness
	cd build ; docker build -f ../DockerfilePrebuiltBinary -t $(DOCKER_IMAGE_REPO):$(BRANCH) .
.PHONY: docker-local-build


arm6: genproto
	GOOS=linux GOARCH=arm GOARM=6 go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)
.PHONY: pi


test: genproto
	ulimit -n 9999; go test -timeout 0 -p 1 ./...
.PHONY: test


test-no-app-test: genproto
	ulimit -n 9999; go test -timeout 0 -p 1 -tags exclude_app_test ./...
.PHONY: test


test-only-app-test: genproto
	ulimit -n 9999; go test -timeout 0 -p 1 -v -tags !exclude_app_test ./cmd/node
.PHONY: test


test-tidy:
	# Working directory must be clean, or this test would be destructive
	git diff --quiet || (echo "\033[0;31mWorking directory not clean!\033[0m" && git diff && exit 1)
	# We expect `go mod tidy` not to change anything, the test should fail otherwise
	make tidy
	git diff --exit-code || (git diff && git checkout . && exit 1)
.PHONY: test-tidy


test-fmt:
	git diff --quiet || (echo "\033[0;31mWorking directory not clean!\033[0m" && git diff && exit 1)
	# We expect `go fmt` not to change anything, the test should fail otherwise
	go fmt ./...
	git diff --exit-code || (git diff && git checkout . && exit 1)
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
	@git diff --quiet || (echo "working directory must be clean"; exit 2)
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


dockerrun-p2p:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif
	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v p2p/test_p2p.py --tc-file=p2p/config.yaml --tc-format=yaml
.PHONY: dockerrun-p2p

dockertest-p2p: dockerbuild-test dockerrun-p2p
.PHONY: dockertest-p2p


dockerrun-mining:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif
	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v test_bs.py --tc-file=config.yaml --tc-format=yaml
.PHONY: dockerrun-mining

dockertest-mining: dockerbuild-test dockerrun-mining
.PHONY: dockertest-mining


dockerrun-hare:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif
	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v hare/test_hare.py::test_hare_sanity --tc-file=hare/config.yaml --tc-format=yaml
.PHONY: dockerrun-hare


dockertest-hare: dockerbuild-test dockerrun-hare
.PHONY: dockertest-hare


dockerrun-sync:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v sync/test_sync.py --tc-file=sync/config.yaml --tc-format=yaml

.PHONY: dockerrun-sync

dockertest-sync: dockerbuild-test dockerrun-sync
.PHONY: dockertest-sync

# command for late nodes

dockerrun-late-nodes:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v late_nodes/test_delayed.py --tc-file=late_nodes/delayed_config.yaml --tc-format=yaml

.PHONY: dockerrun-late-nodes

dockertest-late-nodes: dockerbuild-test dockerrun-late-nodes
.PHONY: dockertest-late-nodes


dockerrun-genesis-voting:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v sync/genesis/test_genesis_voting.py --tc-file=sync/genesis/config.yaml --tc-format=yaml

.PHONY: dockerrun-genesis-voting

dockertest-genesis-voting: dockerbuild-test dockerrun-genesis-voting
.PHONY: dockertest-genesis-voting


dockerrun-blocks-add-node:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v block_atx/add_node/test_blocks_add_node.py --tc-file=block_atx/add_node/config.yaml --tc-format=yaml

.PHONY: dockerrun-blocks-add-node

dockertest-blocks-add-node: dockerbuild-test dockerrun-blocks-add-node
.PHONY: dockertest-blocks-add-node


dockerrun-blocks-remove-node:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v block_atx/remove_node/test_blocks_remove_node.py --tc-file=block_atx/remove_node/config.yaml --tc-format=yaml

.PHONY: dockerrun-blocks-remove-node

dockertest-blocks-remove-node: dockerbuild-test dockerrun-blocks-remove-node
.PHONY: dockertest-blocks-remove-node


dockerrun-blocks-stress:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v stress/blocks_stress/test_stress_blocks.py --tc-file=stress/blocks_stress/config.yaml --tc-format=yaml

.PHONY: dockerrun-blocks-stress

dockertest-blocks-stress: dockerbuild-test dockerrun-blocks-stress
.PHONY: dockertest-blocks-stress


dockerrun-grpc-stress:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v stress/grpc_stress/test_stress_grpc.py --tc-file=stress/grpc_stress/config.yaml --tc-format=yaml

.PHONY: dockerrun-grpc-stress

dockertest-grpc-stress: dockerbuild-test dockerrun-grpc-stress
.PHONY: dockertest-grpc-stress


dockerrun-sync-stress:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v stress/sync_stress/test_sync.py --tc-file=stress/sync_stress/config.yaml --tc-format=yaml

.PHONY: dockerrun-sync-stress

dockertest-sync-stress: dockerbuild-test dockerrun-sync-stress
.PHONY: dockertest-sync-stress


dockerrun-tx-stress:
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif

	docker run --rm -e ES_PASSWD="$(ES_PASSWD)" \
		-e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json \
		-e CLIENT_DOCKER_IMAGE="spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)" \
		-it go-spacemesh-python:$(BRANCH) pytest -s -v stress/tx_stress/test_stress_txs.py --tc-file=stress/tx_stress/config.yaml --tc-format=yaml

.PHONY: dockerrun-tx-stress

dockertest-tx-stress: dockerbuild-test dockerrun-tx-stress
.PHONY: dockertest-tx-stress


# The following is used to run tests one after the other locally
dockerrun-test: dockerbuild-test dockerrun-p2p dockerrun-mining dockerrun-hare dockerrun-sync dockerrun-late-nodes dockerrun-genesis-voting dockerrun-blocks-add-node dockerrun-blocks-add-node dockerrun-blocks-remove-node
.PHONY: dockerrun-test

dockerrun-all: dockerpush dockerrun-test
.PHONY: dockerrun-all

dockerrun-stress: dockerbuild-test dockerrun-blocks-stress dockerrun-grpc-stress dockerrun-sync-stress dockerrun-tx-stress
.PHONY: dockerrun-stress

dockertest-stress: dockerpush dockerrun-stress
.PHONY: dockertest-stress

dockertest-hare-mining: dockertest-hare dockertest-mining
.PHONY: dockertest-hare-mining

dockertest-sync-blocks-remove-node: dockertest-sync dockertest-blocks-remove-node
.PHONY: dockertest-sync-blocks-remove-node

dockertest-genesis-voting-p2p: dockertest-genesis-voting dockertest-p2p
.PHONY: dockertest-genesis-voting-p2p
