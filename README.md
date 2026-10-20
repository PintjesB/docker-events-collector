# Docker Events Collector

Minimal Go Docker container-event collector for Unraid Tower. A separate **Docker socket proxy** exposes only the events endpoint. The collector forwards sanitized RFC5424 TCP syslog to Vector.

- Statically compiled `scratch` image, no shell/interpreter/dependencies, UID 65532.
- Intended Compose runtime: read-only filesystem, capabilities dropped, no new privileges.
- No direct Docker socket mount in the collector.
- Best-effort delivery with possible gaps or duplicates during prolonged disconnects, not forensic capture.

## CI/CD

The public GitHub Actions workflow tests the Go binary and publishes AMD64/ARM64 images at `ghcr.io/pintjesb/docker-events-collector:main` after pushes to main. A release tag can publish a versioned release. GHCR package visibility may require one initial public-visibility setting.

## Configuration

`VECTOR_ADDR` is required, in `host:port` form. `EVENT_HOST` defaults to `tower`. The collector reads events from the private Compose service `docker-socket:2375` and does not read arbitrary Docker API endpoints. See the private `docker-compose/docker-events-tower` stack.
