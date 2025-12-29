#!/bin/bash
set -e

# GoChat Entrypoint Script
# This script preprocesses configuration files to replace localhost addresses
# with Docker service names for multi-container deployment

echo "GoChat Container Starting..."
echo "Module: ${1}"
echo "RUN_MODE: ${RUN_MODE}"

# Set default values if not provided
ETCD_HOST=${ETCD_HOST:-etcd:2379}
REDIS_HOST=${REDIS_HOST:-redis:6379}

echo "ETCD_HOST: ${ETCD_HOST}"
echo "REDIS_HOST: ${REDIS_HOST}"

# Replace etcd addresses in config files
if [ "$ETCD_HOST" != "127.0.0.1:2379" ]; then
    echo "Replacing etcd address 127.0.0.1:2379 with ${ETCD_HOST}..."
    find /app/config -name "*.toml" -type f -exec \
        sed -i "s|127\.0\.0\.1:2379|${ETCD_HOST}|g" {} \;
fi

# Replace Redis addresses in config files (both 127.0.0.1 and localhost)
if [ "$REDIS_HOST" != "127.0.0.1:6379" ]; then
    echo "Replacing Redis addresses with ${REDIS_HOST}..."
    find /app/config -name "*.toml" -type f -exec \
        sed -i "s|127\.0\.0\.1:6379|${REDIS_HOST}|g" {} \;
    find /app/config -name "*.toml" -type f -exec \
        sed -i "s|localhost:6379|${REDIS_HOST}|g" {} \;
fi

# Get container IP address for service registration
CONTAINER_IP=$(hostname -i | awk '{print $1}')
echo "Container IP: ${CONTAINER_IP}"

# Replace RPC binding addresses with container IP for proper service registration
# This allows services to bind AND be discoverable by other containers
echo "Updating RPC addresses to use container IP..."
if [ -n "$CONTAINER_IP" ]; then
    find /app/config -name "logic.toml" -type f -exec \
        sed -i "s|tcp@127\.0\.0\.1:|tcp@${CONTAINER_IP}:|g" {} \;
    find /app/config -name "logic.toml" -type f -exec \
        sed -i "s|tcp@0\.0\.0\.0:|tcp@${CONTAINER_IP}:|g" {} \;

    find /app/config -name "connect.toml" -type f -exec \
        sed -i "s|tcp@0\.0\.0\.0:|tcp@${CONTAINER_IP}:|g" {} \;

    find /app/config -name "task.toml" -type f -exec \
        sed -i "s|tcp@localhost:|tcp@${CONTAINER_IP}:|g" {} \;
    find /app/config -name "task.toml" -type f -exec \
        sed -i "s|tcp@0\.0\.0\.0:|tcp@${CONTAINER_IP}:|g" {} \;
fi

# Handle frontend HOST_IP injection for site module
if [ "$1" = "-module" ] && [ "$2" = "site" ]; then
    if [ -n "$HOST_IP" ]; then
        echo "Updating frontend with HOST_IP: ${HOST_IP}..."
        addr_http=${HOST_IP}:7070
        addr_ws=${HOST_IP}:7000

        # Use extended regex for sed
        if [ -f /app/site/static/js/main.06044d49.js ]; then
            sed -r -i "s|\/\/([0-9]{1,3}\.){3}[0-9]{1,3}:[0-9]+|\/\/${addr_http}|g" /app/site/static/js/main.06044d49.js
            sed -r -i "s|\/\/([0-9]{1,3}\.){3}[0-9]{1,3}:[0-9]+\/ws|\/\/${addr_ws}\/ws|g" /app/site/static/js/main.06044d49.js
            echo "Frontend updated successfully."
        fi
    fi
fi

echo "Configuration preprocessing complete."
echo "Starting GoChat service..."

# Execute the GoChat binary with all provided arguments
exec /app/gochat "$@"
