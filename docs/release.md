# Release Process

AgenSense releases are tag-driven and use GoReleaser.

The release workflow builds cross-platform archives, creates a GitHub Release, uploads checksums, and pushes a Homebrew cask to the tap repository.

## GitHub Requirements

Repository settings:

- `Settings -> Actions -> General -> Workflow permissions`: allow read and write permissions for workflows.
- `Settings -> Secrets and variables -> Actions`: add `HOMEBREW_TAP_GITHUB_TOKEN`.

The release workflow uses the built-in `GITHUB_TOKEN` to create the GitHub Release. `HOMEBREW_TAP_GITHUB_TOKEN` is still required because the workflow must write to a different repository.

For a fine-grained personal access token, grant repository access to `agendash/homebrew-tap` and set:

- `Contents: Read and write`
- `Metadata: Read-only`

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
