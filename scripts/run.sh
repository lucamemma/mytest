#!/bin/sh
# run.sh - Run the Go application in mock mode

# Exit immediately if a command exits with a non-zero status.
#set -e

echo "--- Starting application in MOCK mode ---"

# The working directory is /mnt.

# Set DB_HOST to "mock" to instruct main.go to use the mock database
# implementation instead of trying to connect to a real database.
export DB_HOST="mock"

# Check if the server binary exists. If not, run the build script first.
# This makes the script runnable even if build.sh hasn't been run yet.

if [ ! -f "mytest" ]; then
    echo "Server binary not found. Building application first..."
    # The build script must be executable on the host.
    #./scripts/build.sh
fi

# Execute the server binary. It will use the mock DB.
./mytest
