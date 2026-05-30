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

# Stage an empty state directory for the final image. distroless/static has no
# shell to mkdir/chown, so the writable /state owned by the run uid is copied
# in from here.
RUN mkdir -p /state

# The IOG cardano-node release binaries are fully static musl builds (no
# dynamic loader, no shared libraries) and yacd-cardano-tools is a static
# CGO_ENABLED=0 Go binary, so the final image needs no glibc runtime. A
# distroless static base supplies the only remaining runtime needs — a CA
# bundle for the fetch verb's outbound HTTPS, a nonroot identity, /tmp, and
# tzdata — with no shell, package manager, or libc.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:d093aa3e30dbadd3efe1310db061a14da60299baff8450a17fe0ccc514a16639

ARG CARDANO_NODE_VERSION
ARG YACD_CARDANO_TOOLS_VERSION

LABEL org.opencontainers.image.title="yacd-cardano-tools" \
      org.opencontainers.image.description="YACD Cardano artifact utility (generate, fetch, serve, report) built with official IntersectMBO cardano-node release artifacts" \
      org.opencontainers.image.source="https://github.com/meigma/yacd" \
      org.opencontainers.image.url="https://github.com/meigma/yacd" \
      org.opencontainers.image.version="${YACD_CARDANO_TOOLS_VERSION}" \
      org.opencontainers.image.vendor="Meigma" \
      io.yacd.cardano-node.version="${CARDANO_NODE_VERSION}"

# Copy only the three binaries this tool uses. The release tarball ships ~14
# binaries (~1.2 GB); the three we need are ~370 MB.
COPY --from=fetch /out/bin/cardano-node /out/bin/cardano-cli /out/bin/cardano-testnet /opt/cardano/bin/
COPY --from=build /out/bin/yacd-cardano-tools /opt/yacd/bin/yacd-cardano-tools
COPY --from=build --chown=10001:10001 /state /state

ENV PATH="/opt/yacd/bin:/opt/cardano/bin:${PATH}" \
    CARDANO_CLI="/opt/cardano/bin/cardano-cli" \
    CARDANO_NODE="/opt/cardano/bin/cardano-node"

WORKDIR /state

# Keep the established uid/gid 10001 numerically (distroless/static has no
# shadow utils to create a passwd entry, and these static binaries do not need
# one). Using 10001 rather than the base's 65532 avoids changing /state volume
# ownership expectations. In production /state is a mounted writable volume.
USER 10001:10001

ENTRYPOINT ["yacd-cardano-tools"]
