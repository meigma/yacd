# syntax=docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

ARG CARDANO_NODE_VERSION=11.0.1
ARG YACD_CARDANO_TESTNET_VERSION=11.0.1-yacd.2

FROM --platform=$BUILDPLATFORM debian:bookworm-slim@sha256:0104b334637a5f19aa9c983a91b54c89887c0984081f2068983107a6f6c21eeb AS fetch

ARG CARDANO_NODE_VERSION
ARG TARGETARCH

SHELL ["/bin/sh", "-eux", "-c"]

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates=20230311+deb12u1 \
        curl=7.88.1-10+deb12u14 \
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

FROM --platform=$BUILDPLATFORM golang:1.26.3-bookworm@sha256:386d475a660466863d9f8c766fec64d7fdad3edac2c6a05020c09534d71edb4b AS publisher

ARG TARGETARCH
ARG TARGETOS=linux
ARG YACD_PUBLISHER_VERSION=dev
ARG YACD_PUBLISHER_COMMIT=none
ARG YACD_PUBLISHER_DATE=unknown

WORKDIR /src

COPY publisher/go.mod publisher/go.sum ./
RUN go mod download

COPY publisher/cmd ./cmd
COPY publisher/internal ./internal

RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -trimpath \
      -ldflags "-X main.version=${YACD_PUBLISHER_VERSION} -X main.commit=${YACD_PUBLISHER_COMMIT} -X main.date=${YACD_PUBLISHER_DATE}" \
      -o /out/bin/yacd-cardano-testnet-publisher ./cmd; \
    test -x /out/bin/yacd-cardano-testnet-publisher

FROM debian:bookworm-slim@sha256:0104b334637a5f19aa9c983a91b54c89887c0984081f2068983107a6f6c21eeb

ARG CARDANO_NODE_VERSION
ARG YACD_CARDANO_TESTNET_VERSION

LABEL org.opencontainers.image.title="yacd-cardano-testnet" \
      org.opencontainers.image.description="YACD cardano-testnet tools image built from official IntersectMBO cardano-node release artifacts" \
      org.opencontainers.image.source="https://github.com/meigma/yacd" \
      org.opencontainers.image.url="https://github.com/meigma/yacd" \
      org.opencontainers.image.version="${YACD_CARDANO_TESTNET_VERSION}" \
      org.opencontainers.image.vendor="Meigma" \
      io.yacd.cardano-node.version="${CARDANO_NODE_VERSION}"

RUN groupadd --gid 10001 cardano \
    && useradd --uid 10001 --gid cardano --home-dir /nonexistent --no-create-home --shell /usr/sbin/nologin cardano \
    && mkdir -p /state \
    && chown cardano:cardano /state

COPY --from=fetch /out/bin /opt/cardano/bin
COPY --from=publisher /out/bin/yacd-cardano-testnet-publisher /opt/yacd/bin/yacd-cardano-testnet-publisher
COPY --chmod=0555 yacd-cardano-testnet-init /opt/yacd/bin/yacd-cardano-testnet-init

ENV PATH="/opt/yacd/bin:/opt/cardano/bin:${PATH}" \
    CARDANO_CLI="/opt/cardano/bin/cardano-cli" \
    CARDANO_NODE="/opt/cardano/bin/cardano-node"

WORKDIR /state

USER cardano:cardano

ENTRYPOINT ["cardano-testnet"]
