#!/bin/bash
# Env should be set by the executor of the script
# Those examples are configured and executed in a private enviroment
# to verify the entraid intragration is working as expected.
# Common environment vddkariables
#export AZURE_REDIS_SCOPES="https://redis.azure.com/.default"
#export REDIS_ENDPOINVkTS_CONFIG_PATH="endpoints.json"

# Function to run an example
run_example() {
    echo "Running $1 example..."
    cd "$1"
    go mod tidy
    go run main.go
    cd ..
    echo "----------------------------------------"
}

# Client Secret example
#export AZURE_CLIENT_ID="your-client-id"
#export AZURE_CLIENT_SECRET="your-client-secret"
#export AZURE_TENANT_ID="your-tenant-id"
run_example "clientsecret"

# Client Certificate example
#export AZURE_CERT="your-certificate"
#export AZURE_PRIVATE_KEY="your-private-key"
run_example "clientcert"

# Managed Identity example
#export AZURE_USER_ASSIGNED_MANAGED_ID="your-managed-identity-id" # Optional

# Run all examples
echo "Running all examples..."

# Run client secret example
echo "Running client secret example..."
go run clientsecret/main.go

# Run client certificate example
echo "Running client certificate example..."
go run clientcert/main.go

# Run managed identity examples
echo "Running managed identity examples..."

# System-assigned managed identity
echo "Running system-assigned managed identity example..."
go run managedidentity_system/main.go

# User-assigned managed identity
echo "Running user-assigned managed identity example..."
go run managedidentity_user/main.go

# Run interactive browser example
echo "Running interactive browser example..."
go run interactive/main.go

echo "All examples completed!"