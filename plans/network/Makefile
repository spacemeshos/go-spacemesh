include Makefile.Inc

COMMIT = $(shell git rev-parse HEAD)
SHA = $(shell git rev-parse --short HEAD)
BRANCH ?= $(shell git rev-parse --abbrev-ref HEAD)
LDFLAGS = -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.branch=${BRANCH}"

export GO111MODULE := on
export CGO_ENABLED := 1


ifeq ($(TESTGROUND_PLAN),1)
  BUILDDIR := /build/
else
  BUILDDIR := $(PROJ_DIR)/build/
endif



build: get-gpu-setup
ifeq ($(TESTGROUND_PLAN),1)
	mkdir $(BUILDDIR)
	cp $(PROJ_DIR)/build/* $(BUILDDIR)
endif
	go build -a -o $(BUILDDIR)testplan $(LDFLAGS) $(GOTAGS) .

test: get-gpu-setup
	go test $(LDFLAGS) $(GOTAGS) .
