#!/bin/bash

pkgs=`go list ./...`

#output=`go vet $pkgs` TODO: grep -B does'nt work with grep -v
#if [ "$output" != "" ]; then
#	exit 1
#fi

output=`golint $pkgs 2>&1 | grep -vE "_mock|_test"`
if [ $(echo -n "$output" |  wc -l) -ne 0 ]; then
    echo -n "$output";
    exit 1;
fi

