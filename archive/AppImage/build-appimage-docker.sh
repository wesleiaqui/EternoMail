#!/bin/bash
# Build Aerion AppImage using Docker with Ubuntu 22.04

set -e

if [ "$(id -u)" = 0 ]; then
    echo "Run this build as the normal workspace owner, not root." >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_DIR="${SCRIPT_DIR}/dist"

echo "Building Aerion AppImage in Ubuntu 22.04 container..."
echo "Output directory: ${OUTPUT_DIR}"

# Create output directory
mkdir -p "${OUTPUT_DIR}"

# Build Docker image
echo "Building Docker image..."
docker build -f Dockerfile.ubuntu22.04 -t aerion-builder:ubuntu22.04 .

# Run build in container
echo "Running build..."
docker run --rm \
    --user "$(stat -c %u "$SCRIPT_DIR"):$(stat -c %g "$SCRIPT_DIR")" \
    --env HOME=/tmp \
    -v "${SCRIPT_DIR}:/build" \
    -v "${OUTPUT_DIR}:/output" \
    aerion-builder:ubuntu22.04 \
    bash -c 'test "$(id -u)" != 0 && test "$(id -u):$(id -g)" = "$(stat -c %u:%g /build)" && make appimage && cp build/bin/Aerion-*.AppImage /output/'

echo "Build complete! AppImage is in ${OUTPUT_DIR}"
ls -lh "${OUTPUT_DIR}"/Aerion-*.AppImage
