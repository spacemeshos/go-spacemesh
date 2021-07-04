PROJ_DIR := $(dir $(realpath $(firstword $(MAKEFILE_LIST))))
BIN_DIR ?= $(PROJ_DIR)build/

export CGO_LDFLAGS := -L$(BIN_DIR) -Wl,-rpath,\$$ORIGIN $(CGO_LDFLAGS_EXT)
export GOOS
export GOARCH
export GOARM
export BIN_DIR

ifeq ($(OS),Windows_NT)
  HOST_OS := windows
else
  HOST_OS := $(shell uname | tr [A-Z] [a-z])
endif

ifeq ($(GOOS),)
  GOOS := $(HOST_OS)
endif

ifeq ($(GOOS),windows)
  platform := windows
  export PATH := $(PATH):$(PROJ_DIR)build
  EXE := .exe
else
  TEMP := /tmp
  ifeq ($(GOOS),darwin)
	  platform := macos
  else
      platform := linux
  endif
endif

$(info "OS: $(OS), HOST_OS: $(HOST_OS), GOOS: $(GOOS), GOARCH: $(GOARH), BIN_DIR: $(BIN_DIR), platform: $(platform)")

GPU_SETUP_REV = 0.1.20
GPU_SETUP_ZIP = libgpu-setup-$(platform)-$(GPU_SETUP_REV).zip
GPU_SETUP_URL_ZIP = https://github.com/spacemeshos/gpu-post/releases/download/v$(GPU_SETUP_REV)/$(GPU_SETUP_ZIP)
ifeq ($(platform), windows)
  GPU_SETUP_LIBS = gpu-setup.dll
else
  ifeq ($(platform), macos)
  	GPU_SETUP_LIBS = libgpu-setup.dylib libMoltenVK.dylib libvulkan.1.dylib MoltenVK_icd.json
  else
  	GPU_SETUP_LIBS = libgpu-setup.so
  endif
endif

BINDIR_GPU_SETUP_LIBS = $(foreach X,$(GPU_SETUP_LIBS),$(BIN_DIR)$(X))
$(BINDIR_GPU_SETUP_LIBS): $(PROJ_DIR)$(GPU_SETUP_ZIP)
	mkdir -p $(dir $@)
	unzip -o $(PROJ_DIR)$(GPU_SETUP_ZIP) -d $(dir $@) $(notdir $@)
	touch $@
$(PROJ_DIR)$(GPU_SETUP_ZIP):
	curl -L $(GPU_SETUP_URL_ZIP) -o $(PROJ_DIR)$(GPU_SETUP_ZIP)
$(BIN_DIR)$(BINARY):
	mkdir -p $(dir $@)
	go build ${LDFLAGS} -o $(BIN_DIR)$(BINARY)

get-gpu-setup: $(PROJ_DIR)$(GPU_SETUP_ZIP) $(BINDIR_GPU_SETUP_LIBS)
