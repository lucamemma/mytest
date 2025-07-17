#!/bin/sh
# test.sh - Run the Go tests

# Exit immediately if a command exits with a non-zero status.
#set -e

echo "--- Running Go tests ---"

# The working directory is /mnt.
# Ensure dependencies are downloaded before running tests.
echo "Ensuring dependencies are up to date..."


# Run tests in verbose mode for the current package and all sub-packages.
cd app
go test -v .

echo "--- Tests complete ---"
