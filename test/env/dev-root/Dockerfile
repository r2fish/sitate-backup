# root 前提の試験用コンテナ(受け口 adapters/acquire/recv の SITATE_DESTRUCTIVE_SETUP=1 の試験。root・PostgreSQL 17・sudo・acl が要る)。
# 基底は開発用コンテナ(test/env/dev/Dockerfile)と同じ Go のイメージ(digest 固定、Debian bookworm)。x86_64 では GO_IMAGE を上書きする。
ARG GO_IMAGE=golang@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb
FROM ${GO_IMAGE}
RUN apt-get update \
 && apt-get install -y --no-install-recommends acl git make ca-certificates gcc libc6-dev tar findutils coreutils \
      util-linux sudo procps jq openssh-client shellcheck curl gnupg postgresql-common \
 && /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh -y \
 && apt-get install -y --no-install-recommends postgresql-17 \
 && rm -rf /var/lib/apt/lists/*
ENV PATH=/usr/lib/postgresql/17/bin:$PATH \
    GOFLAGS=-buildvcs=false \
    GOCACHE=/cache/go-build \
    GOMODCACHE=/cache/go-mod \
    HOME=/cache/home
RUN mkdir -p /cache/home && chmod -R 0777 /cache
WORKDIR /work
