# Docker Events Collector

Minimal Go Docker container-event collector forwarding sanitized lifecycle events to the existing Vector TCP syslog listener for Wazuh detection.

## Privacy-preserving command-execution telemetry

The Docker Events API exposes exec actions with full command arguments. Commands can
contain passwords, tokens or other private data. The collector processes the raw
action only in memory and emits *fixed, allowlisted* fields instead:

- `action=exec_start`
- `exec_class`: `shell_direct`, `shell_command`, `privileged_tool`,
  `docker_socket_access`, `network_tool`, `docker_cli` or `other`
- `exec_tool`: a fixed list of known executable basenames or `other`

No raw command, command arguments, environment variables, Docker labels or unknown
binary paths are forwarded. `exec_create` and `exec_die` remain excluded.
High-volume shell `-c` healthchecks can be kept below Wazuh alert thresholds,
while direct shells and high-risk tools can be alerted using Tower-specific
rules. Detection is heuristic: this is not process-level audit or attribution.
Do not copy Docker exec arguments to logs or Wazuh as a fallback.

The image uses a static Go binary in `scratch`, runs as UID 65532, has no shell or package manager, and never mounts Docker's host socket. The separate private Compose repository contains a restricted Docker API proxy.

## Automatic SemVer releases

**No manual version file or Git tag updates.** Existing Git tags are the only source of truth for published image versions.

- PRs run Go race tests, vet, SemVer release algorithm tests, and AMD64/ARM64 builds without publishing.
- Each successful merge/push to `main` computes the next stable version since the highest Git tag `vX.Y.Z`.
- Conventional commit `feat:` or `feat(scope):` increments the **minor** version; `type!:` or a `BREAKING CHANGE:` footer increments **major**; all other merges, including Renovate `chore(deps):`, increment **patch**.
- CI publishes `ghcr.io/pintjesb/docker-events-collector:X.Y.Z` for AMD64/ARM64 with SBOM and provenance, then creates `vX.Y.Z` after publication succeeds.
- Publication is serialized across runs. Older runs that were superseded by a later release are skipped. A commit that already has a release is not republished.
- No `:main` or `:latest` image. The consuming private Compose stack uses a SemVer tag with an immutable digest updated by Renovate.

Even documentation-only merges generate a patch release; use `feat` and `!` only for actual feature and breaking changes. Squash merge titles should follow Conventional Commits to classify versions correctly.

## Renovate

Repository-specific `renovate.json` uses the existing homelab conventions: Europe/Brussels timezone, vulnerability labels, CI-gated non-major dependency automerge, manual review for permission-sensitive GitHub Actions, and Go compiler major/minor approval.

## Configuration

| Variable | Required | Description |
| --- | --- | --- |
| `VECTOR_ADDR` | Yes | Vector TCP syslog listener as `host:port`. Do not use the Wazuh agent port. |
| `EVENT_HOST` | No | Syslog hostname, defaults to `tower`. |

## Limitations

This is best-effort monitoring, not forensic capture. Prolonged Docker/Vector outages can cause gaps or duplicates, and TCP writes do not prove Wazuh ingestion.

## Local validation

```sh
go test -race -count=1 ./...
go vet ./...
python3 -m unittest discover -s tests -v
```
