# 開発用コンテナ(核とアダプタの試験を走らせる Linux 環境)。
# 基底は digest で固定する(Go 1.26.10、Debian bookworm、linux/arm64)。
# x86_64 の端末では GO_IMAGE を build-arg で上書きする(Makefile の GO_IMAGE)。
ARG GO_IMAGE=golang@sha256:23ce71337b726608afb1bc00986e630a05034a87a87ea1fc2cd1ae5cf33387eb
FROM ${GO_IMAGE}
RUN apt-get update \
 && apt-get install -y --no-install-recommends git make minisign ca-certificates curl jq openssh-client shellcheck \
      podman uidmap fuse-overlayfs slirp4netns netavark aardvark-dns crun fuse3 iptables postgresql-client \
 && rm -rf /var/lib/apt/lists/*
ENV GOFLAGS=-buildvcs=false \
    GOCACHE=/cache/go-build \
    GOMODCACHE=/cache/go-mod \
    HOME=/cache/home
RUN mkdir -p /cache/home && chmod -R 0777 /cache
# rootless の容器の試験用(SITATE_NESTED=1 のとき in-linux.sh が --user rootless で入る。作業指示書 5.1b の完了条件 1)。
RUN useradd -m -u 1000 -s /bin/bash rootless \
 && echo 'rootless:100000:65536' > /etc/subuid && echo 'rootless:100000:65536' > /etc/subgid \
 && mkdir -p /home/rootless/.config/containers \
 && printf '[storage]\ndriver = "vfs"\n' > /home/rootless/.config/containers/storage.conf \
 && chown -R rootless:rootless /home/rootless
WORKDIR /work
