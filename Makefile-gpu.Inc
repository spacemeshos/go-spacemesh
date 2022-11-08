PROJ_DIR := $(dir $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR ?= $(PROJ_DIR)build/

export CGO_LDFLAGS := -L$(BIN_DIR)
export CGO_CFLAGS := -I$(PROJ_DIR)build/
export GOOS
export GOARCH
export GOARM
export BIN_DIR

CGO_TEST_LDFLAGS = $(CGO_LDFLAGS) -Wl,-rpath,$(BIN_DIR)

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
  platform := windows
  export PATH := $(PATH):$(PROJ_DIR)build
  EXE := .exe
else
  TEMP := /tmp
  ifeq ($(GOOS),darwin)
    ifeq ($(GOARCH),arm64)
    	platform := macos-m1
    else
    	platform := macos
    endif
    CGO_LDFLAGS := $(CGO_LDFLAGS) -Wl,-rpath,@loader_path
    ULIMIT := ulimit -n 9999;
  else
    ifeq ($(GOARCH),arm64)
    	platform := linux-arm64
    else
    	platform := linux
    endif
    CGO_LDFLAGS := $(CGO_LDFLAGS) -Wl,-rpath,$$ORIGIN
  endif
endif

ifneq ($(VERBOSE),)
  $(info "OS: $(OS), HOST_OS: $(HOST_OS), GOOS: $(GOOS), GOARCH: $(GOARCH), BIN_DIR: $(BIN_DIR), platform: $(platform)")
endif

GPU_SETUP_REV = 0.1.26
GPU_SETUP_ZIP = libgpu-setup-$(platform)-$(GPU_SETUP_REV).zip
GPU_SETUP_URL_ZIP = https://github.com/spacemeshos/gpu-post/releases/download/v$(GPU_SETUP_REV)/$(GPU_SETUP_ZIP)
ifeq ($(platform), windows)
  GPU_SETUP_LIBS = gpu-setup.dll gpu-setup.lib libgpu-setup.a api.h
else
  ifeq ($(platform), $(filter $(platform), macos macos-m1))
    GPU_SETUP_LIBS = libgpu-setup.dylib libMoltenVK.dylib libvulkan.1.dylib MoltenVK_icd.json api.h
  else
    GPU_SETUP_LIBS = libgpu-setup.so api.h
  endif
endif

BINDIR_GPU_SETUP_LIBS = $(foreach X,$(GPU_SETUP_LIBS),$(BIN_DIR)$(X))
$(BINDIR_GPU_SETUP_LIBS): $(PROJ_DIR)$(GPU_SETUP_ZIP)
	mkdir -p $(dir $@)
	unzip -o $(PROJ_DIR)$(GPU_SETUP_ZIP) -d $(dir $@) $(notdir $@)
	touch $@
$(PROJ_DIR)$(GPU_SETUP_ZIP):
	curl -L $(GPU_SETUP_URL_ZIP) -o $(PROJ_DIR)$(GPU_SETUP_ZIP)

get-gpu-setup: $(PROJ_DIR)$(GPU_SETUP_ZIP) $(BINDIR_GPU_SETUP_LIBS)
.PHONY: get-gpu-setup

SUBDIRS_LVL1 := $(foreach X,$(wildcard $(dir $(PROJ_DIR))*/.), $(lastword $(subst /, ,$(dir $(X)))))
SUBDIRS_ONLY := $(sort \
	$(foreach X,$(SUBDIRS_LVL1),\
		$(foreach Y, $(wildcard $(X)/*_test.go), test-$(X)) ))
SUBDIRS_ALL := $(sort \
	$(foreach X,$(SUBDIRS_LVL1),\
		$(foreach Y, $(wildcard $(X)/*/*_test.go), test-$(X)-all ) \
		$(foreach Y, $(wildcard $(X)/*_test.go), test-$(X)-all ) ))
SUBDIRS_LVL2 := $(sort \
	$(foreach X,$(SUBDIRS_LVL1),\
		$(foreach Y, $(wildcard $(X)/*/*_test.go), test-$(X)-$(lastword $(subst /, ,$(dir $(Y))) ))))

# defines test-* targets from subdirs level 1 and 2
$(SUBDIRS_ONLY): get-gpu-setup
	$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" go test -v ./$(@:test-%=%)
$(SUBDIRS_LVL2): get-gpu-setup
	$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" go test -v ./$(subst -,/,$(@:test-%=%))/...
$(SUBDIRS_ALL): get-gpu-setup
	$(ULIMIT) CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)" go test -v ./$(@:test-%-all=%)/...

print-test-targets:
	@for i in test-all test-tidy test-fmt \
		$(sort \
			$(SUBDIRS_LVL2) \
			$(foreach X, $(SUBDIRS_ALL), $(if $(filter $(SUBDIRS_ONLY),$(X:%-all=%)), \
				$(X:%-all=%)[-all],\
				$(X)))); \
	do echo $$i; \
	done
.PHONY: $(SUBDIRS_ALL) $(SUBDIRS_LVL2) $(SUBDIRS_ONLY) print-test-targets

go-env: get-gpu-setup
	go env -w CGO_CFLAGS="$(CGO_CFLAGS)"
	go env -w CGO_LDFLAGS="$(CGO_LDFLAGS)"
.PHONY: go-env

go-env-test: get-gpu-setup
	go env -w CGO_CFLAGS="$(CGO_CFLAGS)"
	go env -w CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)"
.PHONY: go-env-test

print-env: get-gpu-setup
	@echo CGO_CFLAGS="$(CGO_CFLAGS)"
	@echo CGO_LDFLAGS="$(CGO_LDFLAGS)"
.PHONY: print-env

print-test-env: get-gpu-setup
	@echo CGO_CFLAGS="$(CGO_CFLAGS)"
	@echo CGO_TEST_LDFLAGS="$(CGO_TEST_LDFLAGS)"
.PHONY: print-test-env
