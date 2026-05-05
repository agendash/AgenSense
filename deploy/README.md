# Deploy

This directory is reserved for deployment-adjacent assets that are not part of the service runtime.

Current deployment entrypoints live at the repository root:

- `Dockerfile`
- `compose.yaml`
- `compose.localai.yaml`
- `scripts/run-local.sh`
- `scripts/smoke-local.sh`
- `scripts/docker-local.sh`
- `scripts/localai-up.sh`

See [docs/deployment.md](../docs/deployment.md).

Future deploy assets may include:

- systemd units
- launchd plists
- Kubernetes manifests
- production reverse-proxy examples

Only commit deploy assets after they have been exercised locally or documented as examples.
