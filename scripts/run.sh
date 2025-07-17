#!/bin/sh
# run.sh - Run the Go application in mock mode

echo "--- Starting application in MOCK mode ---"

# The working directory is /mnt.

# Set DB_HOST to "mock" to instruct main.go to use the mock database
export DB_HOST="mock"

# Check if the server binary exists

if [ ! -f "mytest" ]; then
    echo "Server binary not found. Run build.sh again and check out for errors!"
fi

# Execute the server binary with mock DB.
./mytest
