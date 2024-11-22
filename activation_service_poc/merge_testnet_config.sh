#!/bin/bash

# Exit on error
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_CONFIG="$SCRIPT_DIR/config.testnet.json"
CLIENT_CONFIG="$SCRIPT_DIR/config.testnet.client.json"
NODE_SERVICE_CONFIG="$SCRIPT_DIR/config.testnet.node-service.json"
CLIENT_OUTPUT="$SCRIPT_DIR/config.testnet.client.merged.json"
NODE_SERVICE_OUTPUT="$SCRIPT_DIR/config.testnet.node-service.merged.json"

# Check if base config exists
if [ ! -f "$BASE_CONFIG" ]; then
    echo "Error: $BASE_CONFIG not found"
    exit 1
fi

# Merge client config if it exists
if [ -f "$CLIENT_CONFIG" ]; then
    # Merge configs using jq
    # The * operator merges objects recursively, keeping the rightmost value for same-named fields
    jq -s '.[0] * .[1]' "$BASE_CONFIG" "$CLIENT_CONFIG" > "$CLIENT_OUTPUT"
    echo "Client config merged successfully to $CLIENT_OUTPUT"
fi

# Merge node-service config if it exists
if [ -f "$NODE_SERVICE_CONFIG" ]; then
    # Merge configs using jq
    jq -s '.[0] * .[1]' "$BASE_CONFIG" "$NODE_SERVICE_CONFIG" > "$NODE_SERVICE_OUTPUT"
    echo "Node service config merged successfully to $NODE_SERVICE_OUTPUT"
fi

# Exit with error if neither config file was found
if [ ! -f "$CLIENT_CONFIG" ] && [ ! -f "$NODE_SERVICE_CONFIG" ]; then
    echo "Error: Neither $CLIENT_CONFIG nor $NODE_SERVICE_CONFIG found"
    exit 1
fi
