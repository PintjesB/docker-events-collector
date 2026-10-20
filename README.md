# Docker Events Collector

Minimal Go Docker container-event collector. Forwards sanitized lifecycle events to the existing Vector TCP syslog listener, with Wazuh detection handled centrally.

The runtime image is based on `scratch`, contains a statically compiled Go binary, runs as UID 65532, and has no package manager or shell. It never mounts Docker's host socket; a dedicated proxy is supplied by the private Compose stack.

## Versioned releases

- `VERSION` is the source of truth for the stable SemVer version, starting at `0.1.0`.
- Every PR must pass Go tests, race detection, vet and multi-architecture image build without publishing.
- When a version bump is merged to `main`, the workflow creates Git tag `vX.Y.Z` and publishes GHCR image `ghcr.io/pintjesb/docker-events-collector:X.Y.Z` with AMD64/ARM64 manifests, SBOM and provenance.
- A manually pushed `vX.Y.Z` Git tag can also publish, but only when its version matches `VERSION`.
- Commits that do not bump `VERSION` skip image publication. No `:main` or `:latest` tag is published.
- GitHub Actions uses scoped `GITHUB_TOKEN` permissions rather than a PAT. Git tags created by Actions do not trigger separate workflows, so tagging and publishing happen in the same job.
- Verify GHCR anonymous pull permissions after the first publication. The consuming Compose stack should pin the image digest and let Renovate update it.

## Renovate

`renovate.json` is the configuration for the Renovate GitHub App. Matches the established homelab conventions: European timezone, vulnerability alerts, labels, patch/pin/digest automerge after CI, manual publishing Actions and Go compiler major/minor changes, and strict configuration validation.

## Configuration

| Variable | Required | Purpose |
| --- | --- | --- |
| `VECTOR_ADDR` | Yes | Existing Vector TCP syslog listener `host:port`. Never Wazuh secure-agent port. |
| `EVENT_HOST` | No | Defaults to `tower`. |

Use the separate private `PintjesB/docker-compose/docker-events-tower` Compose stack for socket-proxy confinement and outbound networking.

## Limitations

Best-effort event capture. Docker's replay buffer and lack of persistent state can cause event gaps or duplicates. Successful TCP writes do not prove Vector persistence or Wazuh ingestion. The collector does not forward raw Docker labels, command strings or environment variables.

## Local checks

```sh
go test -race -count=1 ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o /tmp/docker-events-collector .
```
