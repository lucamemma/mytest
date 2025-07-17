#!/bin/sh
# build.sh - Build the Go application

set -e

echo "--- Building Go application ---"
cd app

go build -o ../ .

echo "--- Build complete. Binary is at mnt/ ---"
