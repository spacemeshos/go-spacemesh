PROJ_DIR := $(dir $(realpath $(firstword $(MAKEFILE_LIST))))
PROJ_DIR := $(subst \,/,$(PROJ_DIR))
BIN_DIR ?= $(PROJ_DIR)build/

export CGO_LDFLAGS := -L$(BIN_DIR)
export CGO_CFLAGS := -I$(BIN_DIR) -I${BIN_DIR}include
export GOOS
export GOARCH
export GOARM
export BIN_DIR

CGO_TEST_LDFLAGS = $(CGO_LDFLAGS) -Wl,-rpath,$(BIN_DIR)

ifeq ($(GOOS),)
	ifeq ($(OS),Windows_NT)
		GOOS := windows
	else
		GOOS := $(shell uname | tr [A-Z] [a-z])
	endif
endif

ifeq ($(GOARCH),)
	GOARCH := $(shell go env GOARCH)
endif

ifeq ($(GOOS),windows)
	platform := windows
	export PATH := $(PATH):$(BIN_DIR)
	CGO_LDFLAGS := $(CGO_LDFLAGS) -Wl,-Bstatic -lpthread -Wl,-Bdynamic
else
	TEMP := /tmp
	ifeq ($(GOOS),darwin)
		ifeq ($(GOARCH),arm64)
				platform := macos-m1
		else
				platform := macos
		endif
		CGO_LDFLAGS := $(CGO_LDFLAGS) -Wl,-rpath,@loader_path
	else
		platform := linux
		ifeq ($(GOARCH),arm64)
				arch := arm64
				libpost_arch := -arm64
		else
				arch := amd64

		endif
		CGO_LDFLAGS := $(CGO_LDFLAGS) -Wl,-rpath,$$ORIGIN
	endif
endif

POSTRS_SETUP_REV = 0.7.13
POSTRS_SETUP_ZIP = libpost-$(platform)$(libpost_arch)-v$(POSTRS_SETUP_REV).zip
POSTRS_SETUP_URL_ZIP ?= https://github.com/spacemeshos/post-rs/releases/download/v$(POSTRS_SETUP_REV)/$(POSTRS_SETUP_ZIP)

POSTRS_PROFILER_ZIP = profiler-$(platform)$(libpost_arch)-v$(POSTRS_SETUP_REV).zip
POSTRS_PROFILER_URL ?= https://github.com/spacemeshos/post-rs/releases/download/v$(POSTRS_SETUP_REV)/$(POSTRS_PROFILER_ZIP)

POSTRS_SERVICE_ZIP = post-service-$(platform)$(libpost_arch)-v$(POSTRS_SETUP_REV).zip
POSTRS_SERVICE_URL ?= https://github.com/spacemeshos/post-rs/releases/download/v$(POSTRS_SETUP_REV)/$(POSTRS_SERVICE_ZIP)

ATHENA_SETUP_REV = 0.3.0-try-uintptr-for-hostcontext
ATHENA_SETUP_ARTIFACT = athena_vmlib_v$(ATHENA_SETUP_REV)_$(GOOS)_$(GOARCH).tar.gz
ATHENA_SETUP_ARTIFACT_URL ?= https://github.com/athenavm/athena/releases/download/v$(ATHENA_SETUP_REV)/$(ATHENA_SETUP_ARTIFACT)

ifeq ($(platform), windows)
	POSTRS_SETUP_LIBS = post.h post.dll

	POSTRS_PROFILER_BIN = profiler.exe
	POSTRS_SERVICE_BIN = post-service.exe

	ATHENA_LIB = libathena_vmlib.dll
	ATHENA_SETUP_LIBS = $(ATHENA_LIB) include/athcon/helpers.h include/athcon/athcon.h
else
	ifeq ($(platform), $(filter $(platform), macos macos-m1))
		POSTRS_SETUP_LIBS = post.h libpost.dylib
		ATHENA_LIB = libathena_vmlib.dylib
		ATHENA_SETUP_LIBS = $(ATHENA_LIB) include/athcon/helpers.h include/athcon/athcon.h
	else
		POSTRS_SETUP_LIBS = post.h libpost.so
		ATHENA_LIB = libathena_vmlib.so
		ATHENA_SETUP_LIBS = $(ATHENA_LIB) include/athcon/helpers.h include/athcon/athcon.h
	endif

	POSTRS_PROFILER_BIN = profiler
	POSTRS_SERVICE_BIN = post-service
endif

BINDIR_POSTRS_SETUP_LIBS = $(foreach X,$(POSTRS_SETUP_LIBS),$(BIN_DIR)$(X))
BINDIR_ATHENA_SETUP_LIBS = $(foreach X,$(ATHENA_SETUP_LIBS),$(BIN_DIR)$(X))

# file marking libpost version
$(BIN_DIR).libpost.version.$(POSTRS_SETUP_REV): $(PROJ_DIR)$(POSTRS_SETUP_ZIP)
	rm -f $(BIN_DIR).libpost.version.*
	mkdir -p $(dir $@)
	touch $@

$(BINDIR_POSTRS_SETUP_LIBS): $(BIN_DIR).libpost.version.$(POSTRS_SETUP_REV)
	mkdir -p $(dir $@)
	unzip -DD -o -j $(PROJ_DIR)$(POSTRS_SETUP_ZIP) -d $(dir $@) $(notdir $@)

CURL_OPTIONS = --retry 10 --retry-max-time 120
CURL_VERSION = $(shell curl --version 2>/dev/null | head -n 1 | cut -d' ' -f2)
ifeq ($(shell expr "$(CURL_VERSION)" \>= 7.71.0),1)
	CURL_OPTIONS := $(CURL_OPTIONS) --retry-all-errors
endif

$(PROJ_DIR)$(ATHENA_SETUP_ARTIFACT):
	curl -sSL $(CURL_OPTIONS) $(ATHENA_SETUP_ARTIFACT_URL) -o $(PROJ_DIR)$(ATHENA_SETUP_ARTIFACT)

# file marking athena version
$(BIN_DIR).athena.version.$(ATHENA_SETUP_REV): $(PROJ_DIR)$(ATHENA_SETUP_ARTIFACT)
	rm -f $(BIN_DIR).athena.version.*
	mkdir -p $(dir $@)
	touch $@

$(BINDIR_ATHENA_SETUP_LIBS): $(BIN_DIR).athena.version.$(ATHENA_SETUP_REV)
	mkdir -p $(dir $@)
	tar xzf $(PROJ_DIR)$(ATHENA_SETUP_ARTIFACT) -C $(BIN_DIR) $(subst $(BIN_DIR),,$@)
	touch $@

# rename athena VM library
ATHENA_LIB_RENAMED := libathenavmwrapper$(suffix $(ATHENA_LIB))

$(BIN_DIR)$(ATHENA_LIB_RENAMED): $(BIN_DIR)$(ATHENA_LIB)
	mv $(BIN_DIR)$(ATHENA_LIB) $@

$(PROJ_DIR)$(POSTRS_SETUP_ZIP):
	curl -sSL $(CURL_OPTIONS) $(POSTRS_SETUP_URL_ZIP) -o $(PROJ_DIR)$(POSTRS_SETUP_ZIP)

$(PROJ_DIR)$(POSTRS_PROFILER_ZIP):
	curl -sSL $(CURL_OPTIONS) $(POSTRS_PROFILER_URL) -o $(PROJ_DIR)$(POSTRS_PROFILER_ZIP)

# file marking profiler version
$(BIN_DIR).profiler.version.$(POSTRS_SETUP_REV): $(PROJ_DIR)$(POSTRS_PROFILER_ZIP)
	rm -f $(BIN_DIR).profiler.version.*
	mkdir -p $(dir $@)
	touch $@

$(BIN_DIR)$(POSTRS_PROFILER_BIN): $(BIN_DIR).profiler.version.$(POSTRS_SETUP_REV)
	unzip -DD -o -j $(PROJ_DIR)$(POSTRS_PROFILER_ZIP) -d $(BIN_DIR)
	chmod +x $(BIN_DIR)$(POSTRS_PROFILER_BIN)

$(PROJ_DIR)$(POSTRS_SERVICE_ZIP):
	curl -sSL $(CURL_OPTIONS) $(POSTRS_SERVICE_URL) -o $(PROJ_DIR)$(POSTRS_SERVICE_ZIP)

# file marking post-service version
$(BIN_DIR).service.version.$(POSTRS_SETUP_REV): $(PROJ_DIR)$(POSTRS_SERVICE_ZIP)
	rm -f $(BIN_DIR).service.version.*
	mkdir -p $(dir $@)
	touch $@

$(BIN_DIR)$(POSTRS_SERVICE_BIN): $(BIN_DIR).service.version.$(POSTRS_SETUP_REV)
	unzip -DD -o -j $(PROJ_DIR)$(POSTRS_SERVICE_ZIP) -d $(BIN_DIR)
	chmod +x $(BIN_DIR)$(POSTRS_SERVICE_BIN)

get-postrs-lib: $(BINDIR_POSTRS_SETUP_LIBS)
get-postrs-profiler: $(BIN_DIR)$(POSTRS_PROFILER_BIN)
get-postrs-service: $(BIN_DIR)$(POSTRS_SERVICE_BIN)
get-athena-lib: $(BINDIR_ATHENA_SETUP_LIBS) $(BIN_DIR)$(ATHENA_LIB_RENAMED)

go-env: get-postrs-lib
	go env -w CGO_CFLAGS="$(CGO_CFLAGS)"
	go env -w CGO_LDFLAGS="$(CGO_LDFLAGS)"
.PHONY: go-env

go-env-test: get-postrs-lib
	go env -w CGO_CFLAGS="$(CGO_CFLAGS)"
	go env -w CGO_LDFLAGS="$(CGO_TEST_LDFLAGS)"
.PHONY: go-env-test

print-env: get-postrs-lib
	@echo CGO_CFLAGS="\"$(CGO_CFLAGS)\"" CGO_LDFLAGS="\"$(CGO_LDFLAGS)\""
.PHONY: print-env

print-test-env: get-postrs-lib
	@echo CGO_CFLAGS="\"$(CGO_CFLAGS)\"" CGO_LDFLAGS="\"$(CGO_TEST_LDFLAGS)\""
.PHONY: print-test-env
