#!/bin/bash
# Env should be set by the executor of the script
# Those examples are configured and executed in a private environment
# to verify the entraid integration is working as expected.
# Common environment variables
#export AZURE_REDIS_SCOPES="https://redis.azure.com/.default"
#export REDIS_ENDPOINTS_CONFIG_PATH="endpoints.json"

# Exit on any error
set -e

# Function to run tests for an example
run_example_test() {
    local example_dir=$1
    echo "Running tests for $example_dir..."
    
    if [ ! -d "$example_dir" ]; then
        echo "Error: Directory $example_dir does not exist"
        return 1
    fi
    
    if [ ! -f "$example_dir/example_test.go" ]; then
        echo "Warning: example_test.go not found in $example_dir, skipping..."
        return 0
    fi
    
    pushd "$example_dir" > /dev/null
    go get github.com/redis/go-redis/v9@master
    go mod tidy
    
    # Run tests with verbose output and JSON format for potential CTRF conversion
    if ! go test -v -timeout 30m .; then
        echo "Error: $example_dir tests failed"
        popd > /dev/null
        return 1
    fi
    
    popd > /dev/null
    echo "----------------------------------------"
    return 0
}

# Track overall success
failed_tests=()
skipped_tests=()

# Run all example tests in the directory
for example in */; do
    # Skip config directory as it's not an example
    if [ "$example" = "config/" ]; then
        continue
    fi
    
    # Skip runscript_test as it already has its own test structure
    if [ "$example" = "runscript_test/" ]; then
        continue
    fi
    
    # Remove trailing slash
    example=${example%/}
    
    # Check if example_test.go exists
    if [ ! -f "$example/example_test.go" ]; then
        skipped_tests+=("$example")
        echo "Skipping $example (no example_test.go found)"
        continue
    fi
    
    if ! run_example_test "$example"; then
        failed_tests+=("$example")
    fi
done

# Report results
echo "========================================"
echo "Test Results Summary"
echo "========================================"

if [ ${#skipped_tests[@]} -gt 0 ]; then
    echo "Skipped tests (no example_test.go):"
    printf '  - %s\n' "${skipped_tests[@]}"
    echo ""
fi

if [ ${#failed_tests[@]} -eq 0 ]; then
    echo "✓ All example tests completed successfully!"
    exit 0
else
    echo "✗ The following example tests failed:"
    printf '  - %s\n' "${failed_tests[@]}"
    exit 1
fi

