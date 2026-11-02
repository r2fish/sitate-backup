# 開発用コンテナ(核とアダプタの試験を走らせる Linux 環境)。
# 基底は digest で固定する(Go 1.26.10、Debian bookworm、linux/arm64)。
# x86_64 の端末では GO_IMAGE を build-arg で上書きする(Makefile の GO_IMAGE)。
ARG GO_IMAGE=golang@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb
FROM ${GO_IMAGE}
RUN apt-get update \
 && apt-get install -y --no-install-recommends git make minisign ca-certificates curl jq openssh-client shellcheck \
 && rm -rf /var/lib/apt/lists/*
ENV GOFLAGS=-buildvcs=false \
    GOCACHE=/cache/go-build \
    GOMODCACHE=/cache/go-mod \
    HOME=/cache/home
RUN mkdir -p /cache/home && chmod -R 0777 /cache
WORKDIR /work
