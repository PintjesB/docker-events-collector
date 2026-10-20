# syntax=docker/dockerfile:1
ARG GO_VERSION=1.27.3
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod *.go ./
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/docker-events .

FROM scratch
LABEL org.opencontainers.image.source="https://github.com/PintjesB/docker-events-collector"
COPY --from=build --chown=65532:65532 /out/docker-events /docker-events
USER 65532:65532
ENTRYPOINT ["/docker-events"]
