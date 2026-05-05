# Release Process

AgenSense releases are tag-driven and use GoReleaser.

The release workflow builds cross-platform archives, creates a GitHub Release, uploads checksums, and pushes a Homebrew cask to the tap repository.

## GitHub Requirements

If the organization allows writable workflow tokens, repository settings can use:

- `Settings -> Actions -> General -> Workflow permissions`: allow read and write permissions for workflows.

If that setting is disabled or locked by the organization, use explicit release tokens instead. The current workflow expects these Actions secrets:

- `RELEASE_GITHUB_TOKEN`: token with write access to `agendash/AgenSense` contents and releases.
- `HOMEBREW_TAP_GITHUB_TOKEN`: token with write access to `agendash/homebrew-tap`.

For a fine-grained personal access token, grant repository access to the target repo and set:

- `Contents: Read and write`
- `Metadata: Read-only`

`RELEASE_GITHUB_TOKEN` must cover:

```text
agendash/AgenSense
```

`HOMEBREW_TAP_GITHUB_TOKEN` must cover:

```text
agendash/homebrew-tap
```

The tap repository should exist before the first release. The Homebrew tap name will be:

```text
agendash/tap
```

## Release A Version

Create and push a semver tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The `Release` GitHub Action will run GoReleaser and publish:

- `agensense`
- `agensense-smoke`
- macOS, Linux, and Windows archives
- `checksums.txt`
- Homebrew cask `Casks/agensense.rb` in `agendash/homebrew-tap`

## Homebrew Install

After the release workflow completes:

```sh
brew install --cask agendash/tap/agensense
agensense -version
agensense-smoke -version
```

## Local Release Check

If GoReleaser is installed locally:

```sh
goreleaser check
goreleaser release --snapshot --clean
```

The snapshot command writes local artifacts under `dist/`, which is ignored by Git.

## Notes

- GoReleaser Homebrew formula publishing is deprecated upstream, so this release path publishes a cask for the prebuilt CLI binaries.
- Keep the version flag fast and non-blocking so users and package managers can validate installs with `agensense -version`.
- The public client integration skill is packaged with release archives under `skills/`.
- Docker image publishing is intentionally not part of the first release workflow. Add it after the binary and Homebrew release path is stable.
