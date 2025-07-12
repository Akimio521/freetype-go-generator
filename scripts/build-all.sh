#!/bin/bash

platforms=(
    "linux/386"
    "linux/amd64"
    "linux/arm/v7"
    "linux/arm64/v8"
    "linux/ppc64le"
    "linux/riscv64"
    "linux/s390x"
)

if [ $# -gt 0 ]; then
    platforms=("$@")
else
    platforms=("${default_platforms[@]}")
fi

for platform in "${platforms[@]}"; do
    echo "Building for $platform..."
    docker run --platform "$platform" --rm -v "$(pwd)":/workspace -w /workspace alpine:latest scripts/build-linux.sh
done