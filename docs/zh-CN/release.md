# 发布流程

AgenSense 使用 tag 驱动发布，发布工作由 GoReleaser 完成。

发布 workflow 会构建跨平台压缩包、创建 GitHub Release、上传 checksums，并把 Homebrew cask 推送到 tap 仓库。

## GitHub 要求

仓库设置：

- `Settings -> Actions -> General -> Workflow permissions`：允许 workflow 读写仓库内容。
- `Settings -> Secrets and variables -> Actions`：添加 `HOMEBREW_TAP_GITHUB_TOKEN`。

发布 workflow 使用内置 `GITHUB_TOKEN` 创建 GitHub Release。`HOMEBREW_TAP_GITHUB_TOKEN` 仍然需要，因为 workflow 要写入另一个仓库。

如果使用 fine-grained personal access token，给 `agendash/homebrew-tap` 授权，并设置：

- `Contents: Read and write`
- `Metadata: Read-only`

`HOMEBREW_TAP_GITHUB_TOKEN` 需要覆盖：

```text
agendash/homebrew-tap
```

第一次发布前需要先创建这个 tap 仓库。对应 Homebrew tap 名称是：

```text
agendash/tap
```

## 发布版本

创建并推送 semver tag：

```sh
git tag v0.1.0
git push origin v0.1.0
```

`Release` GitHub Action 会发布：

- `agensense`
- `agensense-smoke`
- macOS、Linux、Windows 压缩包
- `checksums.txt`
- `agendash/homebrew-tap` 里的 `Casks/agensense.rb`

## Homebrew 安装

发布完成后：

```sh
brew install --cask agendash/tap/agensense
agensense -version
agensense-smoke -version
```

## 本地检查

如果本机安装了 GoReleaser：

```sh
goreleaser check
goreleaser release --snapshot --clean
```

snapshot 产物会写入 `dist/`，该目录不会提交到 Git。

GoReleaser 上游已经把 Homebrew formula 发布标记为 deprecated，所以这里使用 cask 发布预编译 CLI 二进制。
