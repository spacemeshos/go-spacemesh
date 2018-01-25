#!/bin/bash

import_path="github.com/spacemeshos/go-spacemesh"
pkgs=`go list ./... | grep -vF vendor/`
ignored_pkgs="."

for pkg in $pkgs; do
	relative_path="${pkg/$import_path/.}"
	i=0
	for ignore_pkg in $ignored_pkgs; do
		if [ "$ignore_pkg" == "$relative_path" ]; then
			i=1
		fi
		if [ $i -eq 1 ]; then
			continue 2
		fi
	done
	output=`gofmt -s -l $relative_path`
	if [ "$output" != "" ]; then
		echo "validate-gofmt.sh: error $output" 1>&2
		exit 1
	fi
done
