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
	go test -p 1 ./...

.PHONY: build test devtools cover

lint:
	./ci/validate-lint.sh

devtools:
	# Install the build tools
	go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway
	go get -u github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger
    rm -rf $GOPATH/src/github.com/golang/protobuf
    git clone -b v1.2.0 https://github.com/golang/protobuf $GOPATH/src/github.com/golang/protobuf
    go install github.com/golang/protobuf/protoc-gen-go
	go get -u github.com/kardianos/govendor




	# Get the dependencies
	govendor sync
cover:
	@echo "mode: count" > cover-all.out
	@$(foreach pkg,$(PKGS),\
		go test -coverprofile=cover.out -covermode=count $(pkg);\
		tail -n +2 cover.out >> cover-all.out;)
	go tool cover -html=cover-all.out
