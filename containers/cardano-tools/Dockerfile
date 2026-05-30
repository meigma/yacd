# syntax=docker/dockerfile:1.19.0@sha256:b6afd42430b15f2d2a4c5a02b919e98a525b785b1aaff16747d2f623364e39b6

# The build context is the repository ROOT (not this directory): the
# yacd-cardano-tools binary is part of the root Go module so it can import the
# shared artifact contract directly. Build with:
#   docker build -f containers/cardano-tools/Dockerfile .

ARG CARDANO_NODE_VERSION=11.0.1
ARG YACD_CARDANO_TOOLS_VERSION=11.0.1-yacd.0

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

FROM --platform=$BUILDPLATFORM golang:1.26.3-bookworm@sha256:386d475a660466863d9f8c766fec64d7fdad3edac2c6a05020c09534d71edb4b AS build

ARG TARGETARCH
ARG TARGETOS=linux
ARG YACD_CARDANO_TOOLS_VERSION=dev
ARG YACD_CARDANO_TOOLS_COMMIT=none
ARG YACD_CARDANO_TOOLS_DATE=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build -trimpath \
      -ldflags "-X main.version=${YACD_CARDANO_TOOLS_VERSION} -X main.commit=${YACD_CARDANO_TOOLS_COMMIT} -X main.date=${YACD_CARDANO_TOOLS_DATE}" \
      -o /out/bin/yacd-cardano-tools ./containers/cardano-tools/cmd/yacd-cardano-tools; \
    test -x /out/bin/yacd-cardano-tools

FROM debian:bookworm-slim@sha256:0104b334637a5f19aa9c983a91b54c89887c0984081f2068983107a6f6c21eeb

ARG CARDANO_NODE_VERSION
ARG YACD_CARDANO_TOOLS_VERSION

LABEL org.opencontainers.image.title="yacd-cardano-tools" \
      org.opencontainers.image.description="YACD Cardano artifact utility (generate, fetch, serve, report) built with official IntersectMBO cardano-node release artifacts" \
      org.opencontainers.image.source="https://github.com/meigma/yacd" \
      org.opencontainers.image.url="https://github.com/meigma/yacd" \
      org.opencontainers.image.version="${YACD_CARDANO_TOOLS_VERSION}" \
      org.opencontainers.image.vendor="Meigma" \
      io.yacd.cardano-node.version="${CARDANO_NODE_VERSION}"

RUN groupadd --gid 10001 cardano \
    && useradd --uid 10001 --gid cardano --home-dir /nonexistent --no-create-home --shell /usr/sbin/nologin cardano \
    && mkdir -p /state \
    && chown cardano:cardano /state

COPY --from=fetch /out/bin /opt/cardano/bin
COPY --from=build /out/bin/yacd-cardano-tools /opt/yacd/bin/yacd-cardano-tools

ENV PATH="/opt/yacd/bin:/opt/cardano/bin:${PATH}" \
    CARDANO_CLI="/opt/cardano/bin/cardano-cli" \
    CARDANO_NODE="/opt/cardano/bin/cardano-node"

WORKDIR /state

USER cardano:cardano

ENTRYPOINT ["yacd-cardano-tools"]
