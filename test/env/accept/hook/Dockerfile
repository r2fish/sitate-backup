ARG BASE_IMAGE=debian@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587
FROM ${BASE_IMAGE}
RUN apt-get update && apt-get install -y --no-install-recommends python3 openssl ca-certificates && rm -rf /var/lib/apt/lists/*
COPY receiver.py /usr/local/bin/hook-receiver
COPY gen-certs /usr/local/bin/gen-certs
RUN chmod +x /usr/local/bin/hook-receiver /usr/local/bin/gen-certs && mkdir -p /var/lib/hook /etc/hook/tls
ENTRYPOINT ["/usr/local/bin/hook-receiver"]
