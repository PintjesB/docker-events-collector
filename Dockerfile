# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
WORKDIR /src
COPY go.mod *.go ./
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/docker-events .

FROM scratch
LABEL org.opencontainers.image.source="https://github.com/PintjesB/docker-events-collector"
COPY --from=build --chown=65532:65532 /out/docker-events /docker-events
USER 65532:65532
ENTRYPOINT ["/docker-events"]
