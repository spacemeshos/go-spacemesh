#!/bin/bash
export LIBRARY_PATH=$GOPATH/src/github.com/spacemeshos/go-bls/external/mcl/lib		# compile time libs linux, mac
export LD_LIBRARY_PATH=$GOPATH/src/github.com/spacemeshos/go-bls/external/mcl/lib	# run time libs linux
export DYLD_LIBRARY_PATH=$GOPATH/src/github.com/spacemeshos/go-bls/external/mcl/lib	# run time libs mac

