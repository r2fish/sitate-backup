# 受入試験の環境のホストのイメージ(作業指示書 5.1b、3.3)。OS は Debian 12 bookworm、init は systemd(3 役とも)。
# 基底は digest で固定する(linux/arm64。x86_64 の端末では BASE_IMAGE を build-arg で上書きする)。
ARG BASE_IMAGE=debian@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587
FROM ${BASE_IMAGE} AS base
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
 && apt-get install -y --no-install-recommends systemd systemd-sysv dbus openssh-server openssh-client sudo ca-certificates curl iproute2 procps util-linux \
 && rm -rf /var/lib/apt/lists/* \
 && systemctl mask getty.target console-getty.service systemd-udevd.service systemd-udev-trigger.service \
      systemd-hwdb-update.service apt-daily.timer apt-daily-upgrade.timer e2scrub_all.timer fstrim.timer \
 && mkdir -p /run/sshd /opt/sitate-test/in
COPY host/sitate-test-init /usr/local/sbin/sitate-test-init
STOPSIGNAL SIGRTMIN+3
ENTRYPOINT ["/usr/local/sbin/sitate-test-init"]

# 元サーバー: PostgreSQL 17(PGDG の Debian パッケージ。local の peer の行を持つ既定の pg_hba.conf)と source-setup の段 prereq の道具。
FROM base AS source
RUN apt-get update && apt-get install -y --no-install-recommends gnupg jq e2fsprogs acl findutils tar coreutils \
 && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | gpg --dearmor -o /usr/share/keyrings/pgdg.gpg \
 && echo "deb [signed-by=/usr/share/keyrings/pgdg.gpg] https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" > /etc/apt/sources.list.d/pgdg.list \
 && apt-get update && apt-get install -y --no-install-recommends postgresql-17 postgresql-client-17 \
 && rm -rf /var/lib/apt/lists/*
COPY host/gen-source-data /usr/local/sbin/sitate-gen-source-data
COPY host/source-add-extra /usr/local/sbin/sitate-source-add-extra
RUN chmod +x /usr/local/sbin/sitate-gen-source-data /usr/local/sbin/sitate-source-add-extra

# バックアップサーバー: 空の Debian(sitate ユーザーも podman も入っていない。bootstrap が入れる)。systemd のタイマーを使う。
FROM base AS backup

# 復元先(ssh-host): rootless の podman、gocryptfs と FUSE、隔離の設定に要るファイアウォールの道具(spec 3.2.7 の ssh-host の前提)。
FROM base AS dest
RUN apt-get update && apt-get install -y --no-install-recommends podman uidmap fuse-overlayfs slirp4netns crun fuse3 gocryptfs iptables nftables tar \
 && rm -rf /var/lib/apt/lists/*
