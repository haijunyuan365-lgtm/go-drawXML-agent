#!/bin/bash

# Default API URL if not provided
DEFAULT_API_URL="http://localhost:8091/api/v1"
API_URL=${1:-$DEFAULT_API_URL}

echo "Building Docker image with API Base URL: $API_URL"

# Build the Docker image
# We pass the NEXT_PUBLIC_API_BASE_URL as a build argument
docker build --platform linux/amd64,linux/arm64 \
  --build-arg NEXT_PUBLIC_API_BASE_URL="$API_URL" \
  --load -t fuzhengwei/ai-draw-io-front:1.1 .

echo "Build complete. Image tagged as ai-draw-io-front:latest"
