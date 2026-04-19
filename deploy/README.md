# deploy

这个目录目前只应该放两类东西：

- 本地 MVP 的非代码辅助资产，例如 smoke helper、示例请求、演示脚本
- 主线程集成完成之后，已经被实际验证过的部署样例

这轮本地 MVP 明确不要求：

- `docker compose`
- `systemd unit`
- k8s manifests
- edge relay 部署脚本

当前仓库已经有本地单进程 MVP，但还没有实际验证过的 `docker compose`、`systemd` 或 k8s 资产，所以这个目录暂时继续保持克制。
