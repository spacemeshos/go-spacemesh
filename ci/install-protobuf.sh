#!/bin/sh
set -ex
wget https://github.com/google/protobuf/releases/download/v3.6.0/protobuf-all-3.6.0.tar.gz
tar -xzvf protobuf-all-3.6.0.tar.gz
cd protobuf-3.6.0 && ./configure --prefix=/usr && make && sudo make install
