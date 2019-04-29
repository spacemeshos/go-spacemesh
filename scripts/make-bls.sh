#!/bin/bash

# CLONE REPOR TO TMP DIR
BLS_DIR=$GOPATH/src/github.com/spacemeshos/go-bls
mkdir -p $BLS_DIR
pushd $GOPATH/src/github.com/spacemeshos/
if [ ! -d "$BLS_DIR" ]; then
	echo "go-bls does not exist. Cloning repo"
	git clone https://github.com/spacemeshos/go-bls.git
fi

# MAKE MCL
cd go-bls/external/mcl
make clean
make all
# MAKE BLS
cd ../bls
make clean
make all
cp ./lib/* ../mcl/lib
popd

