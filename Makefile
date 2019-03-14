BINARY := go-spacemesh
VERSION := 0.0.1
COMMIT = $(shell git rev-parse HEAD)
SHA = $(shell git rev-parse --short HEAD)
CURR_DIR = $(shell pwd)
CURR_DIR_WIN = $(shell cd)
BIN_DIR = $(CURR_DIR)/build
BIN_DIR_WIN = $(CUR_DIR_WIN)/build
export GO111MODULE = on

ifndef TRAVIS_PULL_REQUEST_BRANCH
	BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
else
	BRANCH := $(TRAVIS_PULL_REQUEST_BRANCH)
endif

# Setup the -ldflags option to pass vars defined here to app vars
LDFLAGS = -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}"

PKGS = $(shell go list ./...)

PLATFORMS := windows linux darwin
os = $(word 1, $@)

DOCKER_IMAGE_NAME=go-spacemesh

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
	go build ${LDFLAGS} -o $(BIN_DIR_WIN)/$(BINARY).exe
else
	make genproto
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

dockerbuild-go:
	docker build -t $(DOCKER_IMAGE_NAME):latest .
.PHONY: dockerbuild-go

dockerpush: dockerbuild-go
	echo "$(DOCKER_PASSWORD)" | docker login -u "$(DOCKER_USERNAME)" --password-stdin
	docker tag $(DOCKER_IMAGE_NAME) spacemeshos/$(DOCKER_IMAGE_NAME):$(BRANCH)
	docker tag $(DOCKER_IMAGE_NAME) spacemeshos/$(DOCKER_IMAGE_NAME):$(SHA)
	# Temporary disable the push
	#docker push spacemeshos/$(DOCKER_IMAGE_NAME):$(BRANCH)
	#docker push spacemeshos/$(DOCKER_IMAGE_NAME):$(SHA)
.PHONY: dockerpush

dockerbuild-test:
	docker build -f DockerFileTests --build-arg GCLOUD_KEY="$(GCLOUD_KEY)" \
	             --build-arg PROJECT_NAME="$(PROJECT_NAME)" \
	             --build-arg CLUSTER_NAME="$(CLUSTER_NAME)" \
	             --build-arg CLUSTER_ZONE="$(CLUSTER_ZONE)" \
	             -t go-spacemesh-python:latest .
.PHONY: dockerbuild-test

dockerrun-test: dockerbuild-test
ifndef ES_PASSWD
	$(error ES_PASSWD is not set)
endif
	docker run -e ES_PASSWD="$(ES_PASSWD)" -e GOOGLE_APPLICATION_CREDENTIALS=./spacemesh.json -it go-spacemesh-python pytest -k test_client -s --tc-file=config.yaml --tc-format=yaml
.PHONY: dockerrun-test
