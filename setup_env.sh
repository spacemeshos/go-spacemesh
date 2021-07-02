#!/usr/bin/env bash
set -e
./scripts/check-go-version.sh

go mod download

GO111MODULE=off go get golang.org/x/lint/golint # TODO: also install on Windows

echo "setup complete 🎉"
