#!/bin/bash

# https://github.com/golang/go/issues/46312

set -e

fuzzTime=${1:-"10s"}

files=$(grep -r --include='**_test.go' --files-with-matches 'func Fuzz' .)

for file in ${files}
do
    funcs=$(grep -oP 'func \K(Fuzz\w*)' $file)
    for func in ${funcs}
    do
        parentDir=$(dirname $file)
        command="go test $parentDir -run=$func -fuzz=$func -fuzztime=${fuzzTime}"
        echo $command
        eval $command
    done
done
