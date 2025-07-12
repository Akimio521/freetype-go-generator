#!/bin/sh

apk update
apk add \
    build-base \
    autoconf \
    automake \
    libtool \
    pkgconfig \
    zlib-dev \
    git \
    curl \
    go

go run cmd/generator/main.go