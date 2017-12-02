GOPATH=$(shell pwd)/vendor:$(shell pwd)
GOBIN=$(shell pwd)/build
SUBDIRS=$(wildcard */)
GOFILES=$(wildcard $(addsuffix *.go,$(subdirs)))
GONAME=$(shell basename "$(PWD)")
PID=/tmp/go-$(GONAME).pid

default: build

build:
	@echo $(GOFILES)
	@echo "Building $(GOFILES) to ./build"
	@GOPATH=$(GOPATH) GOBIN=$(GOBIN) go build -o build/$(GONAME) $(GOFILES)

get:
  @GOPATH=$(GOPATH) GOBIN=$(GOBIN) go get .

install:
  @GOPATH=$(GOPATH) GOBIN=$(GOBIN) go install $(GOFILES)

run:
  @GOPATH=$(GOPATH) GOBIN=$(GOBIN) go run $(GOFILES)

.PHONY: build get install run watch start stop restart clean