#!/bin/bash

pkgs=`go list ./... | grep -vF /vendor/`

go vet $pkgs
if [ $? -eq 1 ]; then
	exit 1
fi

#output=`golint $pkgs`
#if [ "$output" != "" ]; then
#	echo "$output" 1>&2
#	exit 1
#fi
