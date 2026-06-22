# 部署

这份文档记录当前仓库支持的部署路径。

## 本地二进制

构建并运行：

```sh
go build -o bin/agensense ./cmd/agensense
AGENSENSE_ADDR=:8080 \
AGENSENSE_PUBLIC_BASE_URL=http://127.0.0.1:8080 \
AGENSENSE_DATA_DIR=tmp/agensense \
AGENSENSE_DEFAULT_PROVIDER_BASE_URL=http://127.0.0.1:8081/v1 \
./bin/agensense
```

也可以使用：

```sh
./scripts/run-local.sh
```

## Docker Compose

只启动 AgenSense。这个模式假设 LocalAI 可以从容器内通过 `host.docker.internal:8081` 访问。

```sh
docker compose up --build
```

健康检查：

```sh
curl -sS http://127.0.0.1:8080/healthz
```

停止：

```sh
docker compose down
```

删除本地数据卷：

```sh
docker compose down -v
```

## Docker Compose + LocalAI

同时启动 AgenSense 和 LocalAI：

```sh
docker compose -f compose.yaml -f compose.localai.yaml up --build
```

或：

```sh
./scripts/localai-up.sh
```

Compose 内部 AgenSense 使用 `http://localai:8080/v1` 访问 LocalAI。宿主机访问 LocalAI 的地址是 `http://127.0.0.1:8081`。

模型和 API key 说明见 [LocalAI 配置](localai.md)。

## 容器镜像

构建：

```sh
docker build -t agendash/agensense:local .
```

运行：

```sh
docker run --rm \
  -p 8080:8080 \
  -e AGENSENSE_ADDR=:8080 \
  -e AGENSENSE_PUBLIC_BASE_URL=http://127.0.0.1:8080 \
  -e AGENSENSE_DATA_DIR=/data \
  -e AGENSENSE_DEFAULT_PROVIDER_BASE_URL=http://host.docker.internal:8081/v1 \
  --add-host host.docker.internal:host-gateway \
  -v agensense-data:/data \
  agendash/agensense:local
```

## 脚本

`scripts/` 下的脚本保持很薄：

- `scripts/run-local.sh`：用本地默认参数启动服务
- `scripts/smoke-local.sh`：对本地服务运行 voice smoke
- `scripts/docker-local.sh`：运行 `docker compose up --build`
- `scripts/localai-up.sh`：同时运行 AgenSense 和 LocalAI

这些脚本应该进 Git，因为它们记录了预期的本地工作流，避免命令漂移。

## 环境变量

常用运行时变量：

- `AGENSENSE_ADDR`
- `AGENSENSE_PUBLIC_BASE_URL`
- `AGENSENSE_DATA_DIR`
- `AGENSENSE_LOG_LEVEL`
- `AGENSENSE_DEBUG`
- `AGENSENSE_DISABLE_DEMO_SEED`
- `AGENSENSE_DEFAULT_API_KEY`
- `AGENSENSE_DEFAULT_PROVIDER_BASE_URL`
- `AGENSENSE_DEFAULT_PROVIDER_API_KEY`
- `AGENSENSE_DEFAULT_ASR_MODEL`
- `AGENSENSE_DEFAULT_LLM_MODEL`
- `AGENSENSE_DEFAULT_MULTIMODAL_MODEL`（可选；默认继承 `AGENSENSE_DEFAULT_LLM_MODEL`）
- `AGENSENSE_DEFAULT_TTS_MODEL`

## 状态和密钥

当前运行时会把 provider profile 存在本地 JSON store。Provider API key 现在也是明文存储。

生产环境允许不可信用户注册 provider 凭据之前，需要先补真正的密钥管理层。

## 生产缺口

仓库里的 Docker Compose 是本地部署示例，不是完整生产栈。

缺少的生产项：

- 外部持久数据库
- 凭据加密
- 审计日志
- metrics 和 tracing pipeline
- 限流和配额
- TLS termination
- 备份和恢复流程
