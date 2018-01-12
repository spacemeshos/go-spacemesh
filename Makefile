BINARY := go-spacemesh
VERSION := 0.0.1
COMMIT :=$(shell git rev-parse HEAD)
BRANCH :=$(shell git rev-parse --abbrev-ref HEAD)
BIN_DIR := $(shell pwd)/build
CURR_DIR :=$(shell pwd)

# Setup the -ldflags option to pass vars defined here to app vars
LDFLAGS = -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}"

PKGS := $(shell go list ./... | grep -v /vendor)

PLATFORMS := windows linux darwin
os = $(word 1, $@)

build:
	go build ${LDFLAGS} -o $(CURR_DIR)/$(BINARY)

.PHONY: $(PLATFORMS)

$(PLATFORMS):
	GOOS=$(os) GOARCH=amd64 go build ${LDFLAGS} -o $(BIN_DIR)/$(BINARY)-$(VERSION)-$(os)-amd64

test:
	#govendor test +local
	go test ./...

.PHONY: build test

devtools:
	# Install the build tools
	go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
	go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger
	go get -u github.com/golang/protobuf/protoc-gen-go
	# Get the dependencies
	go get -u github.com/kardianos/govendor
	govendor sync

cover:
	# Check for cover :
	go test ./$(pkg) -coverprofile=cover.out -covermode=count; go tool cover -html=cover.out
