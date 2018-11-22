#!/bin/sh

# Compile the .proto files to .go types
protoc --go_out=. ./*.proto
