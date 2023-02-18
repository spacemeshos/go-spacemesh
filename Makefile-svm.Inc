PROJ_DIR := $(dir $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR ?= $(PROJ_DIR)build/
export GOOS
export GOARCH
export GOARM
export BIN_DIR

SVM_VERSION := 0.0.35

export CGO_CFLAGS := $(CGO_CFLAGS) -I${BIN_DIR}

ifeq ($(OS),Windows_NT)
	HOST_OS := windows
else
	HOST_OS := $(shell uname | tr [A-Z] [a-z])
endif

ifeq ($(GOOS),)
	GOOS := $(HOST_OS)
endif

ifeq ($(GOARCH),)
	GOARCH := $(shell go env GOARCH)
endif

ifeq ($(GOOS),windows)
	PLATFORM := windows
	SVM_CLI := svm-cli.exe
	export CGO_LDFLAGS := $(CGO_LDFLAGS) -L$(BIN_DIR) -lsvm
	export PATH := $(PATH):$(BIN_DIR)
else
	SVM_CLI := svm-cli
	ifeq ($(GOOS),darwin)
		ifeq ($(GOARCH),arm64)
			PLATFORM := macos-m1
		else
			PLATFORM := macos
		endif
    	export CGO_LDFLAGS := $(CGO_LDFLAGS) $(BIN_DIR)/libsvm.a -lm -ldl -framework Security -framework Foundation
	else
    	PLATFORM := linux
    	export CGO_LDFLAGS := $(CGO_LDFLAGS) $(BIN_DIR)/libsvm.a -lm -ldl -Wl,-rpath,$(BIN_DIR)
	endif
endif

CGO_TEST_LDFLAGS := $(CGO_LDFLAGS) -Wl,-rpath,$(BIN_DIR)

SVM_ZIP = libsvm-$(PLATFORM)-$(SVM_VERSION).zip

$(PROJ_DIR)$(SVM_ZIP):
	echo "Building svm-$(PLATFORM)..."
	mkdir -p $(BIN_DIR)/
	curl -L https://github.com/spacemeshos/svm/releases/download/v$(SVM_VERSION)/svm-$(PLATFORM)-v$(SVM_VERSION).zip -o $(PROJ_DIR)$(SVM_ZIP)
	unzip $(PROJ_DIR)$(SVM_ZIP) -d $(BIN_DIR)/
	chmod +x $(BIN_DIR)/$(SVM_CLI)
	ls $(BIN_DIR)

get-svm: $(PROJ_DIR)$(SVM_ZIP)
.PHONY: get-svm

go-env: get-gpu-setup
	go env -w CGO_LDFLAGS="$(CGO_LDFLAGS)"
.PHONY: go-env

print-ldflags: get-gpu-setup
	@echo $(CGO_LDFLAGS)
.PHONY: print-ldflags

print-test-ldflags: get-gpu-setup
	@echo $(CGO_TEST_LDFLAGS)
.PHONY: print-test-ldflags
