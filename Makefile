BINARY := go-spacemesh
VERSION := 0.0.1
COMMIT = $(shell git rev-parse HEAD)
SHA = $(shell git rev-parse --short HEAD)
CURR_DIR = $(shell pwd)
CURR_DIR_WIN = $(shell cd)
BIN_DIR = $(CURR_DIR)/build
BIN_DIR_WIN = $(CUR_DIR_WIN)/build
export GO111MODULE = on

BRANCH := $(shell bash -c 'if [ "$$TRAVIS_PULL_REQUEST" == "false" ]; then echo $$TRAVIS_BRANCH; else echo $$TRAVIS_PULL_REQUEST_BRANCH; fi')

# Set BRANCH when running make manually
ifeq ($(BRANCH),)
BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
endif

# Setup the -ldflags option to pass vars defined here to app vars
LDFLAGS = -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}"

PKGS = $(shell go list ./...)

PLATFORMS := windows linux darwin
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
	cd cmd/hare ; go build -o $(BIN_DIR_WIN)/go-hare.exe; cd ..
else
	cd cmd/hare ; go build -o $(BIN_DIR)/go-hare; cd ..
endif
.PHONY: hare

p2p:
ifeq ($(OS),WINDOWS_NT)
	cd cmd/p2p ; go build -o $(BIN_DIR_WIN)/go-p2p.exe; cd ..
else
	cd cmd/p2p ; go build -o $(BIN_DIR)/go-p2p; cd ..
endif
.PHONY: p2p

sync:
ifeq ($(OS),WINDOWS_NT)
	cd cmd/sync ; go build -o $(BIN_DIR_WIN)/go-sync.exe; cd ..
else
	cd cmd/sync ; go build -o $(BIN_DIR)/go-sync; cd ..
endif
.PHONY: sync

tidy:
	go mod tidy
.PHONY: tidy

$(PLATFORMS): genproto
ifeq ($(OS),Windows_NT)
	set GOOS=$(os)&&set GOARCH=amd64&&go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)
else
	GOOS=$(os) GOARCH=amd64 go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)
endif
.PHONY: $(PLATFORMS)

test:
	ulimit -n 500; go test -short -p 1 ./...
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

dockerpush: dockerbuild-go
	echo "$(DOCKER_PASSWORD)" | docker login -u "$(DOCKER_USERNAME)" --password-stdin
	docker tag $(DOCKER_IMAGE_REPO):$(BRANCH) spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)
	docker push spacemeshos/$(DOCKER_IMAGE_REPO):$(BRANCH)

ifeq ($(BRANCH),develop)
	docker tag $(DOCKER_IMAGE_REPO):$(BRANCH) spacemeshos/$(DOCKER_IMAGE_REPO):$(SHA)
	docker push spacemeshos/$(DOCKER_IMAGE_REPO):$(SHA)
endif
.PHONY: dockerpush


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

# The following is used to run tests one after the other locally
dockerrun-test: dockerbuild-test dockerrun-p2p dockerrun-mining dockerrun-hare
.PHONY: dockerrun-test

dockerrun-all: dockerpush dockerrun-test
.PHONY: dockerrun-all

