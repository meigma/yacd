# Build the faucet binary.
FROM golang:1.26.3 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

WORKDIR /workspace
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w -buildid= -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o yacd-faucet ./services/faucet/cmd/yacd-faucet

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/yacd-faucet /yacd-faucet
USER 65532:65532

ENTRYPOINT ["/yacd-faucet"]
