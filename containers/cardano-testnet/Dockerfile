# syntax=docker/dockerfile:1

ARG CARDANO_NODE_VERSION=11.0.1

FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS fetch

ARG CARDANO_NODE_VERSION
ARG TARGETARCH

SHELL ["/bin/sh", "-eux", "-c"]

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
    && rm -rf /var/lib/apt/lists/*

RUN case "${TARGETARCH}" in \
        amd64|arm64) release_arch="${TARGETARCH}" ;; \
        *) echo "unsupported target architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    archive="cardano-node-${CARDANO_NODE_VERSION}-linux-${release_arch}.tar.gz"; \
    base_url="https://github.com/IntersectMBO/cardano-node/releases/download/${CARDANO_NODE_VERSION}"; \
    curl -fsSL "${base_url}/${archive}" -o "/tmp/${archive}"; \
    curl -fsSL "${base_url}/cardano-node-${CARDANO_NODE_VERSION}-sha256sums.txt" -o /tmp/sha256sums.txt; \
    grep " ${archive}$" /tmp/sha256sums.txt > /tmp/cardano-node.sha256; \
    cd /tmp; \
    sha256sum -c /tmp/cardano-node.sha256; \
    mkdir -p /out; \
    tar -xzf "/tmp/${archive}" -C /out; \
    test -x /out/bin/cardano-testnet; \
    test -x /out/bin/cardano-cli; \
    test -x /out/bin/cardano-node

FROM debian:bookworm-slim

ARG CARDANO_NODE_VERSION

LABEL org.opencontainers.image.title="yacd-cardano-testnet" \
      org.opencontainers.image.description="YACD cardano-testnet tools image built from official IntersectMBO cardano-node release artifacts" \
      org.opencontainers.image.source="https://github.com/meigma/yacd" \
      org.opencontainers.image.url="https://github.com/meigma/yacd" \
      org.opencontainers.image.version="${CARDANO_NODE_VERSION}" \
      org.opencontainers.image.vendor="Meigma"

COPY --from=fetch /out/bin /opt/cardano/bin

ENV PATH="/opt/cardano/bin:${PATH}" \
    CARDANO_CLI="/opt/cardano/bin/cardano-cli" \
    CARDANO_NODE="/opt/cardano/bin/cardano-node"

WORKDIR /state

ENTRYPOINT ["cardano-testnet"]
