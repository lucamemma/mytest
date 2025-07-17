#!/bin/sh
# test.sh - Run the Go tests

set -e

echo "--- Running Go tests ---"

echo "Ensuring dependencies are up to date..."

cd app
go test -v .

echo "--- Tests complete ---"
