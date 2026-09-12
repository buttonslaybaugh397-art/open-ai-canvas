# 1Panel 部署文件

本目录可独立用于镜像部署，不需要上传源码、Dockerfile 或额外的 Compose 文件。

## 文件

| 文件 | 用途 |
| --- | --- |
| `docker-compose.yml` | 完整编排，包含 PostgreSQL、Redis、数据库迁移、后端和网页 |
| `.env.example` | 可公开的环境配置模板，不含真实密码，默认 `latest` |
| `.env`（仅本地） | 私有恢复配置，已填入当前数据库凭据、`latest` 标签和此前部署访问地址；不提交仓库，使用前核对地址 |
| `README.md` | 部署与数据保护说明 |

## 启动前配置

- `CANVAS_IMAGE_TAG`：默认 `latest`，前端、后端和迁移使用我们自己仓库的最新版镜像，无需手填版本号；需要锁定版本时可改成已发布的共同 `sha-xxxxxxx` 标签。`latest` 指仓库已发布版本，不是本机未发布源码；推送后应等待镜像工作流成功，再更新编排。本编排不会自动构建或发布镜像，也不是 `sha-f88d6d9` 回退模板，镜像需包含 `migrate-schema` 与就绪检查接口。
- `CANVAS_CORS_ORIGINS`：实际浏览器访问地址，例如 `https://canvas.example.com`，不要填写路径、末尾斜杠、Markdown 链接或 `*`。
- 默认仓库 `CANVAS_IMAGE_OWNER=buttonslaybaugh397-art`，没有切换到上游镜像。使用私有仓库时，在 1Panel 配置拉取凭据，不把凭据写入编排。

默认通过宿主机 `127.0.0.1:6868` 接入反向代理并使用 HTTPS。直接 IP 测试时设置 `CANVAS_BIND_ADDRESS=0.0.0.0`，并将 CORS 设置为实际 `http://服务器IP:6868`。不要对公网开放 PostgreSQL、Redis 或后端端口。

## 1Panel 使用

1. 在“容器 → 编排”创建或编辑编排，将 `docker-compose.yml` 整份放入编排编辑器，不要追加到旧 YAML 末尾。
2. 将 `.env` 中的配置填入编排环境变量；若使用目录部署，确保 `.env` 与编排文件位于同一个实际运行目录。
   从仓库下载的目录只有 `.env.example`，需先复制为 `.env` 并填写实际配置；本机已有的私有 `.env` 不要覆盖。
3. 更新已有部署前，先备份并按下一节核对四个数据卷。直接更新原编排，不要让两套 PostgreSQL 同时挂载同一个数据卷。
4. 配置检查通过后启动。迁移容器退出码 0 表示正常完成，随后后端和网页启动。

在本目录执行以下命令只验证配置，不启动服务；未填写必填项时失败是预期保护：

```bash
docker compose --env-file .env -f docker-compose.yml config --quiet
```

若 1Panel 提示 `CANVAS_CORS_ORIGINS is missing a value`，说明编排解析时没有读取到该变量，尚未进入容器启动阶段。在该编排的环境变量中添加实际浏览器 Origin（包含协议和非默认端口），或确认已填写的 `.env` 位于 1Panel 实际使用的编排目录；仅在本机填写或推送 `.env.example` 不会同步服务器配置。编辑器模式请在环境变量栏配置，不要将 `KEY=value` 文本追加到 YAML。直接 IP 访问还需 `CANVAS_BIND_ADDRESS=0.0.0.0`，反向代理部署按实际网络保留回环绑定。不要改为 `*` 或移除 CORS 校验。

外层反向代理需保留 `Host`、`X-Forwarded-Proto`、`X-Forwarded-For`，并为实际使用的 SSE 路径配置流式转发。反向代理若运行在独立容器中，其 `127.0.0.1` 不是宿主机，应使用可到达宿主机的网络与地址。

网页 Nginx 从原始 `Host` 读取完整主机名和端口，并覆盖客户端自带的 `X-Forwarded-Host`；普通 API、SSE、分享和 OAuth 回调统一处理。旧网页镜像可能丢失端口，更新前可在 `backend.environment` 配置完整 `CANVAS_CORS_ORIGINS` 并更新编排重建后端；仅修改文件或普通重启不会改变已有容器环境变量。外层反向代理也需保留公开端口。

## 旧部署数据保护

`.env` 的数据库凭据来自当前正在使用的挂载数据库运行配置，已通过要求密码认证的 TCP 连接验证，没有修改数据库密码或执行恢复。该文件包含真实密码，已被 Git 忽略并限制本机访问权限；不要公开上传、提交仓库、贴出内容或无脱敏的 `docker compose config` 输出。传到 Linux 服务器后将权限限制为仅部署账号可读写（例如 `chmod 600 .env`）。

这里保存的是当前挂载库的有效凭据，不保证与其他日期的整库物理备份密码一致。恢复物理数据卷时，密码必须与备份中的数据库角色匹配；恢复逻辑 dump 时，连接凭据由目标数据库决定。若已有 secrets 卷保存了不同密码，编排会明确拒绝启动，不会自动覆盖。不要删除原 secrets 卷来绕过校验，先核对数据与凭据来源。

默认卷名来自此前的编排，更新前必须在旧容器挂载信息中确认，不能把默认值当作自动识别结果。卷名不匹配会创建空卷。

| 变量 | 默认值 | 需保留的内容 |
| --- | --- | --- |
| `CANVAS_POSTGRES_VOLUME` | `open-ai-canvas_postgres-data` | PostgreSQL 17 数据 |
| `CANVAS_BACKEND_VOLUME` | `open-ai-canvas_backend-data` | 上传文件及 `.settings-key` 加密密钥 |
| `CANVAS_SECRETS_VOLUME` | `open-ai-canvas_deployment-secrets` | 数据库密码和连接串 |
| `CANVAS_REDIS_VOLUME` | `open-ai-canvas_redis-data` | Redis AOF 数据 |

- 原部署使用宿主机目录时，应按原挂载修改编排，不能把目录路径填入卷名变量。
- 本目录已固定 `POSTGRES_PASSWORD` 为当前挂载库的有效密码，恢复该库时不要留空重新生成。旧库必须保留对应 secrets 卷，或填写匹配的原密码；手动密码限至少 32 位字母与数字，其他格式不能直接套用此模板。改环境变量不等于修改数据库密码。
- `.settings-key` 与数据库密码不同，必须保留，否则原存储或渠道凭据可能无法解密。
- 不要将 SQLite、SQL dump 或压缩包直接挂入 PostgreSQL 数据目录，也不要跨 PostgreSQL 主版本直接挂载数据文件。
- 不要删除旧卷、执行 `docker compose down -v` 或清空 Redis AOF。本目录仅包含部署文件及 `.env` 中的数据库凭据，不包含数据库备份或 `.settings-key`，也不会挂载本机测试数据库。

## 更新与验收

应用镜像默认使用我们仓库的 `latest`，迁移、后端和前端均设置 `pull_policy: always`。在 1Panel 执行编排启动或更新时，会检查并拉取该标签当前指向的镜像，不只复用本地存量镜像；普通容器重启不会拉取，也不会在运行期间自动检查更新。由 1Panel 管理升级，不接入 Host Updater 或 Docker socket。

仓库工作流在默认分支发布时更新 `latest`。前后端为独立发布任务，升级前应确认两者均已成功且对应同一提交，避免发布中途版本不一致。需要可复现部署时，仍可用共同 SHA 标签固定版本。

Redis 启动宽限只能容纳 AOF 恢复耗时，不能修复损坏、权限或磁盘问题。迁移失败先查看 `migrate` 日志，Redis unhealthy 先查看 Redis 日志，保留原数据再处理。迁移后不能盲目回退旧应用镜像。

健康检查通过后，还需验证登录、历史资源、个人统计、存储配置解密、显示名称保存以及生成轮询与刷新恢复。
