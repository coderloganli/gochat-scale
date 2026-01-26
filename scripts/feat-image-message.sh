#!/bin/bash

# Functional test script for image message feature
# This script tests the complete image message flow including:
# - Image upload to MinIO
# - Sending image messages (single chat and room)
# - Retrieving image messages in history

set -e

API_URL="${API_URL:-http://localhost:7070}"
MINIO_URL="${MINIO_URL:-http://localhost:9000}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Wait for services to be ready
wait_for_service() {
    local url=$1
    local name=$2
    local max_attempts=30
    local attempt=1

    log_info "Waiting for $name to be ready..."
    while [ $attempt -le $max_attempts ]; do
        if curl -s -f "$url" > /dev/null 2>&1; then
            log_info "$name is ready!"
            return 0
        fi
        echo -n "."
        sleep 2
        attempt=$((attempt + 1))
    done
    log_error "$name is not ready after $max_attempts attempts"
    return 1
}

# Check if MinIO is available
check_minio() {
    log_info "Checking MinIO availability..."
    if curl -s -f "${MINIO_URL}/minio/health/live" > /dev/null 2>&1; then
        log_info "MinIO is available"
        return 0
    else
        log_warn "MinIO is not available, image upload tests will be skipped"
        return 1
    fi
}

# Register a user and return the auth token
register_user() {
    local username=$1
    local password=$2

    local response=$(curl -s -X POST "${API_URL}/user/register" \
        -H "Content-Type: application/json" \
        -d "{\"userName\": \"$username\", \"passWord\": \"$password\"}")

    local code=$(echo "$response" | jq -r '.code')
    if [ "$code" != "0" ]; then
        log_error "Failed to register user $username: $response"
        return 1
    fi

    echo "$response" | jq -r '.data.authToken'
}

# Send an image message to a room
send_room_image() {
    local auth_token=$1
    local room_id=$2
    local image_url=$3

    local response=$(curl -s -X POST "${API_URL}/push/pushRoom" \
        -H "Content-Type: application/json" \
        -d "{\"authToken\": \"$auth_token\", \"msg\": \"$image_url\", \"roomId\": $room_id, \"contentType\": \"image\"}")

    local code=$(echo "$response" | jq -r '.code')
    if [ "$code" != "0" ]; then
        log_error "Failed to send room image: $response"
        return 1
    fi

    log_info "Room image message sent successfully"
    return 0
}

# Send a text message to a room
send_room_text() {
    local auth_token=$1
    local room_id=$2
    local msg=$3

    local response=$(curl -s -X POST "${API_URL}/push/pushRoom" \
        -H "Content-Type: application/json" \
        -d "{\"authToken\": \"$auth_token\", \"msg\": \"$msg\", \"roomId\": $room_id}")

    local code=$(echo "$response" | jq -r '.code')
    if [ "$code" != "0" ]; then
        log_error "Failed to send room text: $response"
        return 1
    fi

    log_info "Room text message sent successfully"
    return 0
}

# Get room history
get_room_history() {
    local auth_token=$1
    local room_id=$2

    local response=$(curl -s -X POST "${API_URL}/push/history/room" \
        -H "Content-Type: application/json" \
        -d "{\"authToken\": \"$auth_token\", \"roomId\": $room_id, \"limit\": 20, \"offset\": 0}")

    local code=$(echo "$response" | jq -r '.code')
    if [ "$code" != "0" ]; then
        log_error "Failed to get room history: $response"
        return 1
    fi

    echo "$response"
}

# Upload image to MinIO (if available)
upload_image() {
    local auth_token=$1
    local image_path=$2

    local response=$(curl -s -X POST "${API_URL}/push/uploadImage" \
        -F "authToken=$auth_token" \
        -F "image=@$image_path")

    local code=$(echo "$response" | jq -r '.code')
    if [ "$code" != "0" ]; then
        log_error "Failed to upload image: $response"
        return 1
    fi

    echo "$response" | jq -r '.data.imageUrl'
}

# Create a simple test PNG image
create_test_image() {
    local output_path=$1

    # Create a 1x1 pixel red PNG using printf (minimal valid PNG)
    printf '\x89PNG\r\n\x1a\n' > "$output_path"
    printf '\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde' >> "$output_path"
    printf '\x00\x00\x00\x0cIDATx\x9cc\xf8\xcf\xc0\x00\x00\x00\x03\x00\x01\x00\x05\xfe\xd4' >> "$output_path"
    printf '\x00\x00\x00\x00IEND\xaeB`\x82' >> "$output_path"

    log_info "Created test image at $output_path"
}

# Main test flow
main() {
    log_info "=========================================="
    log_info "Image Message Feature - Functional Test"
    log_info "=========================================="

    # Wait for API to be ready
    wait_for_service "${API_URL}/metrics" "API"

    # Generate unique test data
    local timestamp=$(date +%s)
    local test_user="imgtest_${timestamp}"
    local test_room=$((1000 + timestamp % 1000))

    log_info "Test parameters:"
    log_info "  User: $test_user"
    log_info "  Room: $test_room"

    # Step 1: Register test user
    log_info ""
    log_info "Step 1: Registering test user..."
    local auth_token=$(register_user "$test_user" "testpass123")
    if [ -z "$auth_token" ] || [ "$auth_token" == "null" ]; then
        log_error "Failed to get auth token"
        exit 1
    fi
    log_info "Registered user with token: ${auth_token:0:16}..."

    # Step 2: Send text message (baseline) and verify API response
    log_info ""
    log_info "Step 2: Sending text message..."
    local text_response=$(curl -s -X POST "${API_URL}/push/pushRoom" \
        -H "Content-Type: application/json" \
        -d "{\"authToken\": \"$auth_token\", \"msg\": \"Hello, this is a text message\", \"roomId\": $test_room}")

    local text_code=$(echo "$text_response" | jq -r '.code')
    if [ "$text_code" != "0" ]; then
        log_error "Text message send failed with code: $text_code"
        log_error "Response: $text_response"
        exit 1
    fi
    log_info "Text message sent successfully (API returned code: 0)"

    # Step 3: Send image message
    log_info ""
    log_info "Step 3: Sending image message..."
    local image_url="http://example.com/test-image-${timestamp}.jpg"
    send_room_image "$auth_token" "$test_room" "$image_url"

    # Step 4: Test image upload (if MinIO is available)
    log_info ""
    log_info "Step 4: Testing image upload..."
    if check_minio; then
        local temp_image="/tmp/test_image_${timestamp}.png"
        create_test_image "$temp_image"

        local uploaded_url=$(upload_image "$auth_token" "$temp_image")
        if [ -n "$uploaded_url" ] && [ "$uploaded_url" != "null" ]; then
            log_info "Image uploaded to: $uploaded_url"

            # Send the uploaded image as a message
            send_room_image "$auth_token" "$test_room" "$uploaded_url"
        else
            log_warn "Image upload returned no URL"
        fi

        rm -f "$temp_image"
    fi

    # Step 5: Verify messages in history
    log_info ""
    log_info "Step 5: Verifying messages in history..."
    sleep 1  # Wait for messages to be persisted

    local history=$(get_room_history "$auth_token" "$test_room")
    log_info "Room history:"
    echo "$history" | jq '.data.messages[] | {content: .content, contentType: .contentType}'

    # Verify contentType field exists
    local has_image_type=$(echo "$history" | jq '.data.messages[] | select(.contentType == "image")' | wc -l)
    local has_text_type=$(echo "$history" | jq '.data.messages[] | select(.contentType == "text")' | wc -l)

    if [ "$has_image_type" -gt 0 ]; then
        log_info "Found image message(s) with contentType='image'"
    else
        log_error "No image messages found with contentType='image'"
        exit 1
    fi

    if [ "$has_text_type" -gt 0 ]; then
        log_info "Found text message(s) with contentType='text'"
    else
        log_warn "No text messages found with contentType='text' (may use default)"
    fi

    log_info ""
    log_info "=========================================="
    log_info "All tests passed successfully!"
    log_info "=========================================="
}

# Run main function
main "$@"
