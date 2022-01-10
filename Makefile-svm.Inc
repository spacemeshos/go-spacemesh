PROJ_DIR := $(dir $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR ?= $(PROJ_DIR)build/
export GOOS
export GOARCH
export GOARM
export BIN_DIR

SVM_VERSION := 0.0.31

export CGO_CFLAGS := $(CGO_CFLAGS) -I${BIN_DIR}

ifeq ($(OS),Windows_NT)
	HOST_OS := windows
else
	HOST_OS := $(shell uname | tr [A-Z] [a-z])
endif

ifeq ($(GOOS),)
	GOOS := $(HOST_OS)
endif

ifeq ($(GOOS),windows)
	PLATFORM := windows
	SVM_CLI := svm-cli.exe
	export CGO_LDFLAGS := $(CGO_LDFLAGS) -L$(BIN_DIR) -lsvm
	export PATH := $(PATH):$(BIN_DIR)
else
	SVM_CLI := svm-cli
	ifeq ($(GOOS),darwin)
    	PLATFORM := macos
    	export CGO_LDFLAGS := $(CGO_LDFLAGS) $(BIN_DIR)/libsvm.a -lm -ldl -framework Security -framework Foundation
	else
    	PLATFORM := linux
    	export CGO_LDFLAGS := $(CGO_LDFLAGS) $(BIN_DIR)/libsvm.a -lm -ldl -Wl,-rpath,$(BIN_DIR)
	endif
endif

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
