#!/bin/sh
# build.sh - Build the Go application

# Exit immediately if a command exits with a non-zero status.
set -e

echo "--- Building Go application ---"

# The working directory is /mnt, set by the 'docker run -w' command.
# go build automatically downloads dependencies if they are missing.
cd app

go build -o ../ .

echo "--- Build complete. Binary is at mnt/ ---"
