# 本地启动手册

本地默认路径分两层：

```text
Docker: postgres + redis + qdrant + minio + minio-init
Host:   auth + file + knowledge + ai-gateway + qa + document + gateway + frontend
```

`services/parser` 已由 Knowledge 的 RAGFlow runtime 方案替代，不再作为本地后端服务启动。

## 直接启动

先安装 Docker、Go `1.25.x`、Bun、`psql` 客户端和 `curl`。
Go 必须安装在实际运行这些脚本的宿主机环境中；如果使用 WSL 启动脚本，Windows
里的 Go 不等于 WSL 里的 Go。

脚本会优先读取 `deploy/.env` 中的 `GOPROXY` / `GOSUMDB`。如果本机网络访问
`proxy.golang.org` 不稳定，默认复制 `deploy/.env.example` 即可；如果还想把同样配置
持久写入当前宿主机环境的 Go 全局配置，可以在同一个 shell 中执行：

```bash
go version
go env GOPROXY
go env -w GOPROXY=https://goproxy.cn,direct
go env -w GOSUMDB=sum.golang.google.cn
```

```bash
cp deploy/.env.example deploy/.env
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh

cd apps/web
bun install
bun run dev
```

日常再次启动时，已经执行过 `bun install` 可以直接：

```bash
./scripts/local/dev-up.sh
./scripts/local/run-backend.sh
cd apps/web && bun run dev
```

停止后端：

```bash
./scripts/local/stop-backend.sh
```

清空本地 infra 数据：

```bash
./scripts/local/stop-backend.sh
docker compose -f deploy/docker-compose.yml --env-file deploy/.env down -v
```

## 配置来源

`deploy/.env.example` 是唯一默认配置来源。用户只复制一次：

```bash
cp deploy/.env.example deploy/.env
```

脚本不会生成、改写或维护另一套默认变量。它们只读取 `deploy/.env`，让宿主机
Go 进程拿到同一份本地配置。Go modules 下载默认读取 `deploy/.env` 里的
`GOPROXY` / `GOSUMDB`；`dev-up.sh` 执行 goose migration、`run-backend.sh`
启动 Go 后端时都会把这两个变量传给 host-run Go 命令。旧 `deploy/.env` 缺少这两项时，
脚本会在本次进程使用仓库默认 Go 镜像并输出提示；如果日志里出现 `proxy.golang.org`
超时，说明后端可能没有真正启动，先修正 `deploy/.env` 后重新运行
`./scripts/local/run-backend.sh`。

如需使用 Go 官方默认值，不要只删除 `deploy/.env` 中的 `GOPROXY` / `GOSUMDB`；
当前脚本在缺失且全局 Go 配置仍是官方默认时会回退到仓库默认镜像。请在本机
`deploy/.env` 显式设置 `GOPROXY=https://proxy.golang.org,direct` 和
`GOSUMDB=sum.golang.org`，或设置企业内部 Go proxy / checksum DB。

默认 demo 账号：

```text
admin / LocalDemoAdmin#12345
superadmin / LocalDemoAdmin#12345
```

`deploy/.env.example` 已经内置中国大陆开发网络默认镜像源。需要直连 Docker Hub 时，
从 `deploy/.env` 删除 `POSTGRES_IMAGE`、`REDIS_IMAGE`、`QDRANT_IMAGE`、
`MINIO_IMAGE` 和 `MINIO_MC_IMAGE` 这几行即可回到 Compose 里的 Docker Hub pinned tags。

`UV_DEFAULT_INDEX` 控制宿主机 uv 在解析或重锁依赖时使用的 Python 包索引，默认使用
清华 PyPI 镜像以加速 Knowledge runtime 依赖准备。`services/parser` 已退役，默认路径
不再准备 standalone Parser；解析、切块、embedding、索引和检索通过
`services/knowledge-runtime` 完成。无法访问清华源的环境应先按网络/代理路径解决；如必须
使用 PyPI 或自建源，需要确保 Knowledge runtime lock/config 与本地启动契约同步。

`GOPROXY` 和 `GOSUMDB` 控制宿主机 Go 工具链下载模块和校验数据库的路径，默认使用
`https://goproxy.cn,direct` 和 `sum.golang.google.cn`，用于 `dev-up.sh` 中的
goose migration 以及 `run-backend.sh` 中的 Go 服务 `go run`。如果
`.local/logs/auth.log`、`.local/logs/gateway.log` 等日志出现
`Get "https://proxy.golang.org/...": i/o timeout`，确认当前 `deploy/.env` 是否包含这
两行；旧环境需要从 `deploy/.env.example` 手动补入。无法访问该 Go 镜像的环境可以在
本机 `deploy/.env` 中改为企业代理；如需直连 Go 官方默认值，请显式设置
`GOPROXY=https://proxy.golang.org,direct` 和 `GOSUMDB=sum.golang.org`。

## 脚本职责

`./scripts/local/dev-up.sh`：

- 校验 `deploy/docker-compose.yml`。
- 先检查同一宿主机环境中的 Docker、Go、`psql` 和必要的 `curl`，缺失时直接
  在命令行报错，避免跑到 migration/seed 中途才失败。
- 拉取 infra 镜像，启动并等待 `postgres`、`redis`、`qdrant`、`minio`
  Compose health checks 通过。
- 单独运行一次性 `minio-init` 创建/校验 `software-teamwork-local` 和
  `software-teamwork-knowledge` bucket；`minio-init` 正常退出不会阻断后续
  migration/seed，非零失败时会提示查看 `docker compose logs minio-init`。
- 如果 `QDRANT_URL` 已设置，则创建或校验 `QDRANT_COLLECTION`。
- 在宿主机执行各服务 goose migration。
- migration 前检查有效 Go module 配置；旧 `deploy/.env` 缺少 `GOPROXY` / `GOSUMDB`
  时，本次进程会使用仓库默认 Go 镜像并提示补齐本地配置。
- 用 `psql` 依次应用本地 demo 数据、AI Gateway profile 和 QA Document MCP
  注册 seed。Document MCP seed 只保存 endpoint/alias 等非敏感元数据；token 来自
  `deploy/.env` 的 `MCP_SERVER_TOKEN`。
- 命令行会输出每个阶段的开始、成功和失败摘要；失败摘要包含当前阶段和后续排查入口。

`./scripts/local/run-backend.sh`：

- 启动任何服务前，先在每个 Go 服务目录执行 `go mod download` 预检模块下载。优先用
  当前 `deploy/.env` 的 `GOPROXY` / `GOSUMDB`；旧 `deploy/.env` 缺少这两项时，本次
  进程会使用仓库默认 Go 镜像并提示补齐本地配置。下载失败或超时时直接在终端打印错误
  和修复提示，不再继续伪装成后端已启动。
- 按顺序启动 `auth`、`file`、`knowledge`、`ai-gateway`、`qa`、`document`、`gateway`。
- Knowledge 运行 `cmd/adapter`，并通过 `VENDOR_RUNTIME_URL` 调用 RAGFlow runtime。
- Go 服务启动使用宿主机 `go run`；Go 模块下载走 `deploy/.env` 里的
  `GOPROXY` / `GOSUMDB`，不是 Docker registry，也不是 `UV_DEFAULT_INDEX`。
- 服务 fork 后默认观察 8 秒；若某个进程组很快退出，脚本会汇总对应
  `.local/logs/<service>.log` 尾部并以非零状态退出。可用
  `LOCAL_BACKEND_STARTUP_CHECK_SECONDS` 调整观察窗口。
- 日志写入 `.local/logs/`，进程组 leader PID 写入 `.local/run/`，供
  `stop-backend.sh` 停掉 `go run` 及其子进程。
- 命令行会输出开始、成功和失败摘要；失败时优先按终端提示处理，再查看服务日志。

`./scripts/local/stop-backend.sh`：

- 按 `.local/run/*.pid` 停止 host-run 后端进程组并清理 pid 文件。
- 即使没有 `.local/run/` 或没有 pid 文件，也会明确输出“nothing to stop”并成功退出。
- 命令行会输出开始、成功和失败摘要；失败时检查 `.local/run/*.pid` 和残留进程。

## Knowledge / RAGFlow

Knowledge 文档上传、解析、切块、embedding、索引和检索通过 RAGFlow runtime 完成。
默认本地栈只启动 Knowledge adapter；真实 ingestion/retrieval 需要显式启动 runtime
及其外部依赖。本地 Knowledge adapter 读取：

```text
VENDOR_RUNTIME_URL=http://127.0.0.1:9380
VENDOR_RUNTIME_SERVICE_TOKEN=local-dev-runtime-service-token-change-me
KNOWLEDGE_RUNTIME_SERVICE_TOKEN=local-dev-runtime-service-token-change-me
KNOWLEDGE_AUTO_START_INGESTION=false
```

runtime API 和 worker 在宿主机启动；本地默认 adapter 使用
`http://127.0.0.1:9380`。tenant-scoped runtime API 需要 `X-Service-Token`，
由 Knowledge adapter 使用 `VENDOR_RUNTIME_SERVICE_TOKEN` 自动转发。
不要再启动 `services/parser`，也不要把 runtime 放回根级 Compose。

启用真实 ingestion 前，需要先准备：

- 可访问的 doc engine。当前 vendored RAGFlow runtime 默认支持
  `DOC_ENGINE=elasticsearch`，但根级 Compose 不启动 Elasticsearch；请使用宿主机
  或外部 Elasticsearch，并让 `services/knowledge-runtime/conf/service_conf.yaml`
  的 `es.hosts` 指向它。
- 可用的 embedding provider。设置 `KNOWLEDGE_RUNTIME_EMBEDDING_FACTORY`、
  `KNOWLEDGE_RUNTIME_EMBEDDING_MODEL`、`KNOWLEDGE_RUNTIME_EMBEDDING_BASE_URL` 和
  `KNOWLEDGE_RUNTIME_MODEL_API_KEY`；只有可信本地 provider 才可显式设置
  `KNOWLEDGE_RUNTIME_ALLOW_EMPTY_MODEL_API_KEY=1`。
- 将 `KNOWLEDGE_AUTO_START_INGESTION=true` 写入本地 `deploy/.env`。

## 快速确认

```bash
curl --noproxy '*' -fsS http://localhost:8080/healthz
curl --noproxy '*' -fsS http://localhost:8080/readyz
curl --noproxy '*' -fsS http://localhost:8083/healthz
```

`http://localhost:8083/readyz` 在 runtime 未启动或 runtime 外部依赖未配置时返回
`503 degraded` 是预期行为；真实 ingestion/retrieval 配好后再用它确认 Knowledge
adapter 到 runtime 的链路。

`http://localhost:8086/readyz` 在 placeholder profile 下返回 `503 degraded` 是预期行为，
表示还没配置真实模型 provider credential，不代表 AI Gateway 进程失败。
默认本地模型 profile 的 OpenAI-compatible 地址是 `http://localhost:11434/v1`。
Document MCP 的默认 endpoint 是 `http://localhost:8085/mcp`，QA 将其工具暴露为
`document__<tool>`；完整工具参数和 Agent 工作流见
[Document MCP 工具契约](../docs/services/document/docs/mcp-tools.md)。

## Seed Data

`dev-up.sh` 按顺序应用 `deploy/seeds/001-local-demo-seed.sql`、
`deploy/seeds/002-ai-gateway-model-profiles.sql`、
`deploy/seeds/003-qa-document-mcp.sql` 和
`deploy/seeds/004-qa-default-knowledge-base.sql`。

Seeded local resources:

| Area | Deterministic resource |
| --- | --- |
| Auth | user `usr_local_admin`, username `admin`, password `LocalDemoAdmin#12345`, role `admin` |
| Auth permissions | `admin:model-profile:write`, `admin:parser-config:write`, `qa:settings:read`, and `qa:settings:write` |
| Knowledge | knowledge base `kb_local_demo`, document `doc_local_demo_seed`, chunk `chunk_local_demo_seed_001` |
| Document | material `22222222-2222-4222-8222-222222222201`, report `22222222-2222-4222-8222-222222222301`, outline `22222222-2222-4222-8222-222222222401` |
| QA | conversation `33333333-3333-4333-8333-333333333301`, user message `33333333-3333-4333-8333-333333333401`, assistant message `33333333-3333-4333-8333-333333333402`, default KB `kb_local_demo` |
| AI Gateway | optional placeholder profiles `default-chat`, `default-embedding`, and `default-rerank` |

`minio-init` 会创建两个本地 bucket：`software-teamwork-local` 供 File service
使用，`software-teamwork-knowledge` 供 RAGFlow Knowledge runtime 使用。

`001-local-demo-seed.sql` 里的本地管理员密码 hash 是
`LOCAL_ADMIN_PASSWORD=LocalDemoAdmin#12345` 对应的 `argon2id` PHC 字符串，
参数为 `m=65536`、`t=3`、`p=2`、16-byte salt、32-byte key。轮换本地密码时，
需要一起更新 `deploy/.env.example`、`001-local-demo-seed.sql` 和本文档，然后重新
运行 `./scripts/local/dev-up.sh` 让 host-run seed SQL 生效。不要把 demo 密码或 hash 用在共享环境或长期环境。

## 排障入口

- Docker 拉取慢、registry rewrite、daemon mirror、proxy 和 WSL 内存：
  [docs/runbooks/docker-image-pull-environment.md](../docs/runbooks/docker-image-pull-environment.md)
- 本地联调顺序、端口和故障判断：
  [docs/runbooks/local-integration.md](../docs/runbooks/local-integration.md)

后端启动后，可以通过 Gateway 确认 seed 管理员和权限：

```bash
curl --noproxy '*' -fsS http://localhost:8080/api/v1/sessions \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"LocalDemoAdmin#12345"}'
```

响应应包含 `admin` 角色，以及 `admin:model-profile:write`、
`admin:parser-config:write`、`qa:settings:read` 等本地演示权限。拿到 token 后，
可以请求 `/api/v1/admin/parser-configs` 或 `/api/v1/admin/model-profiles` 确认
Gateway 管理路由鉴权正常。

只清理本地 demo seed 数据：

```bash
set -a
source deploy/.env
set +a
psql "$POSTGRES_ADMIN_URL" -v ON_ERROR_STOP=1 -f deploy/seeds/099-local-demo-cleanup.sql
```

完整重置本地 infra 数据：

```bash
./scripts/local/stop-backend.sh
docker compose -f deploy/docker-compose.yml --env-file deploy/.env down -v
./scripts/local/dev-up.sh
```

AI Gateway 的本地 placeholder profile 只用于 readiness 检查，里面不是可用的真实
provider key。真正调用模型前，需要运维或开发者配置真实 provider credential。默认
OpenAI-compatible 地址是 `http://host.docker.internal:11434/v1`。

## Request ID 排障

所有服务都会返回或透传 `X-Request-Id`。

```bash
rid=req_local_debug_001
curl --noproxy '*' -fsS http://localhost:8080/readyz -H "X-Request-Id: ${rid}"
rg "${rid}" .local/logs
```

前端问题先记录响应里的 `requestId` 或 `X-Request-Id`，再查 Gateway 日志。如果
Gateway 报依赖错误，用同一个 id 继续查 owner service 日志。

## Knowledge 集成确认

Knowledge 主路径通过 Gateway 暴露：

```bash
curl --noproxy '*' -fsS http://localhost:8080/api/v1/knowledge-bases \
  -H "Authorization: Bearer ${token}" \
  -H "X-Request-Id: req_knowledge_local_001"
curl --noproxy '*' -fsS http://localhost:8080/api/v1/knowledge-bases/kb_local_demo/documents \
  -H "Authorization: Bearer ${token}"
curl --noproxy '*' -fsS http://localhost:8080/api/v1/documents/<documentId>/chunks \
  -H "Authorization: Bearer ${token}"
curl --noproxy '*' -fsS http://localhost:8080/api/v1/knowledge-queries \
  -H "Authorization: Bearer ${token}" \
  -H 'Content-Type: application/json' \
  -d '{"query":"local demo","topK":3}'
```

Knowledge 路由依赖 `VENDOR_RUNTIME_URL` 指向 RAGFlow runtime。runtime 负责 PDF
解析、切块、embedding、索引和检索；不要再启动 `services/parser`。

宿主机启动 runtime API 和 worker：

```bash
cd services/knowledge-runtime
uv sync --python 3.13 --frozen
set -a && . ../../deploy/.env && set +a
# Ensure Elasticsearch is reachable at conf/service_conf.yaml es.hosts first.
# Then set a real embedding provider in your shell or local deploy/.env.
./deploy/api/run-local.sh
./deploy/worker/run-local.sh
```

更多 runtime 说明见 [services/knowledge-runtime/README.md](../services/knowledge-runtime/README.md)。

## Common Dependency Failures

| Symptom | Likely cause | Check |
| --- | --- | --- |
| `gateway /readyz` returns `503 dependency_error` | Redis, auth, or required owner service base URL configuration is not ready | `docker compose ps`, service logs under `.local/logs/` |
| `auth /readyz` returns `postgres unavailable` | Auth migration or PostgreSQL failed | `docker compose logs postgres`; check `.local/logs/auth.log` |
| Knowledge upload/query returns `502 dependency_error` | RAGFlow runtime unreachable or ES/MinIO not ready | Check `VENDOR_RUNTIME_URL`, runtime API, worker, Elasticsearch, and MinIO |
| QA message call fails on model invocation | AI Gateway profile is not running, fake local credential is still in use, or host provider is not listening on `host.docker.internal:11434` | Check `.local/logs/ai-gateway.log` and `.local/logs/qa.log` |
| MinIO bucket missing | `minio-init` did not complete or one of `software-teamwork-local` / `software-teamwork-knowledge` is absent | `docker compose logs minio minio-init` |
| Host port conflict | Another local process uses a default port | Change the matching `*_PORT` in `deploy/.env` |
