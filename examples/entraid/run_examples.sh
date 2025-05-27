#!/bin/bash
# Env should be set by the executor of the script
# Those examples are configured and executed in a private enviroment
# to verify the entraid intragration is working as expected.
# Common environment vddkariables
#export AZURE_REDIS_SCOPES="https://redis.azure.com/.default"
#export REDIS_ENDPOINVkTS_CONFIG_PATH="endpoints.json"

# Exit on any error
set -e

# Function to run an example
run_example() {
    local example_dir=$1
    echo "Running $example_dir example..."
    
    if [ ! -d "$example_dir" ]; then
        echo "Error: Directory $example_dir does not exist"
        return 1
    fi
    
    if [ ! -f "$example_dir/main.go" ]; then
        echo "Error: main.go not found in $example_dir"
        return 1
    fi
    
    pushd "$example_dir" > /dev/null
    go mod tidy
    if ! go run main.go; then
        echo "Error: $example_dir example failed"
        popd > /dev/null
        return 1
    fi
    popd > /dev/null
    echo "----------------------------------------"
    return 0
}

# Track overall success
failed_examples=()

# Run all examples in the directory
for example in */; do
    # Skip config directory as it's not an example
    if [ "$example" = "config/" ]; then
        continue
    fi
    
    # Remove trailing slash
    example=${example%/}
    
    if ! run_example "$example"; then
        failed_examples+=("$example")
    fi
done

# Report results
echo "----------------------------------------"
if [ ${#failed_examples[@]} -eq 0 ]; then
    echo "All examples completed successfully!"
    exit 0
else
    echo "The following examples failed:"
    printf '%s\n' "${failed_examples[@]}"
    exit 1
fi
