# 1Panel 部署文件

本目录可独立用于镜像部署，不需要上传源码、Dockerfile 或额外的 Compose 文件。

## 文件

| 文件 | 用途 |
| --- | --- |
| `docker-compose.yml` | 完整编排，包含 PostgreSQL、Redis、数据库迁移、后端和网页 |
| `.env.example` | 可公开的环境配置模板，不含真实密码，默认 `latest` |
| `Caddyfile.example` | 宿主机 Caddy 配置，默认反代到 `127.0.0.1:3000` |
| `migrate-to-caddy.sh` | Linux 服务器上一键将已有 1Panel 入口从旧端口迁移到 Caddy；复用原编排和数据卷 |
| `upgrade-1panel.sh` | Linux 服务器上一键升级已有 1Panel 编排；先迁移数据库，再重建后端和网页 |
| `.env`（不随仓库分发） | 部署环境的私有配置，保留已有凭据、卷名及地址，不提交仓库 |
| `README.md` | 部署与数据保护说明 |
| `render-editor.mjs` | 在本机从已核对的私有配置生成已有部署专用的编辑器 YAML；需要 Bun 与 Docker Compose，仅解析配置 |

## 已有部署的编辑器模式

`docker-compose.yml` 是需要环境变量的目录版模板，不是脱离配置后仍可直接启动的单文件。1Panel 编辑 YAML 和编辑环境变量是两个输入；只替换 YAML 不会自动读取本机 `.env`。部分版本更新时会用环境变量栏重写服务器 `.env`，因此不能将该栏留空后假定原文件会保留。

已有这套五服务部署时，可在仓库根目录生成不依赖 1Panel 环境变量栏的私有编辑器版：

```bash
bun 1panel-deploy/render-editor.mjs --env-file 1panel-deploy/.env
```

输出为 Git 忽略的 `.local/1panel-deploy/docker-compose.editor.yml`；用 `--image-tag sha-<已发布完整提交SHA>` 固定已发布的共同版本，用 `--output` 指定新输出路径。脚本拒绝覆盖已有输出，不打印原始配置或密码，不启动 Docker 服务，也不修改输入 `.env`。

生成文件明确写入访问 Origin、端口、镜像、数据库名称和原卷名，保留容器内 shell 的 `$$` 转义。它不嵌入数据库密码；四个卷设为 `external: true`，且要求 secrets 卷中已有有效密码文件。卷缺失或密码文件缺失时失败，不创建空卷或生成新密码。此模式不适用于全新安装、宿主机目录挂载或需要补回密码文件的恢复操作。

本机配置不是服务器事实源。使用前核对公开访问地址、代理网络、端口、数据库名称和四个实际卷名；生成文件包含部署地址等私有信息，不提交仓库。升级仍需备份并先停旧应用，再迁移、验收，不能把 `depends_on` 当作自动停旧应用。

## 启动前配置

- `CANVAS_IMAGE_TAG`：默认 `latest`，前端、后端和迁移使用我们自己仓库的最新版镜像，无需手填版本号；需要锁定版本时可改成已发布的共同 `sha-xxxxxxx` 标签。`latest` 指仓库已发布版本，不是本机未发布源码；推送后应等待镜像工作流成功，再更新编排。本编排不会自动构建或发布镜像，也不是 `sha-f88d6d9` 回退模板，镜像需包含 `migrate-schema` 与就绪检查接口。
- `CANVAS_CORS_ORIGINS`：同源访问可留空；跨域访问时填写实际浏览器 Origin，例如 `https://canvas.example.com`，不要填写路径、末尾斜杠、Markdown 链接或 `*`。
- `CANVAS_PUBLIC_BASE_URL`：填写真实 HTTPS 根地址，例如 `https://canvas.example.com`；本地存储给模型上游的短时资源链接依赖此值，不要填写 `/api` 或容器地址。
- 默认仓库 `CANVAS_IMAGE_OWNER=buttonslaybaugh397-art`，没有切换到上游镜像。使用私有仓库时，在 1Panel 配置拉取凭据，不把凭据写入编排。

新部署默认通过宿主机 `127.0.0.1:3000` 接入 Caddy，并由 Caddy 负责 HTTPS。直接 IP 测试时设置 `CANVAS_BIND_ADDRESS=0.0.0.0`，并将 `CANVAS_HTTP_PORT` 与 CORS 设置为实际测试地址。不要对公网开放 PostgreSQL、Redis 或后端端口。

## 已有编排一键升级

`upgrade-1panel.sh` 用于更新现有五服务编排中的应用镜像，不负责网关迁移，也不改变现有 `CANVAS_HTTP_PORT`。它只拉取 `migrate`、`backend`、`web`，先执行目标版本的 `migrate-schema up` 与 `verify`，成功后再重建后端和网页；PostgreSQL、Redis 和四个已有卷保持运行和复用。

脚本要求先有一份**可恢复的数据备份**。`--backup-confirmed` 只是操作者确认，不会伪造或生成卷备份；1Panel 备份失败时不要使用该参数绕过保护。建议使用已发布的共同版本标签，并用 `--expected-revision` 锁定提交：

```bash
sudo bash 1panel-deploy/upgrade-1panel.sh \
  --project-dir /1233/open-ai-canvas-main \
  --compose-file docker-compose.1panel.yml \
  --image-tag 1.5.7.1 \
  --expected-revision eba014162c8c4b80bf1370da9e7c79ebffa8fbf0 \
  --backup-confirmed \
  --yes
```

运行前先用 `--dry-run` 检查路径、Compose 配置、运行中的四个卷和 Web 端口。脚本默认要求现有 Web 仍绑定 `6868`；如果部署实际使用其他端口，必须显式传入 `--web-port`。脚本不会执行删除卷的 Compose 操作，不会创建第二个项目，不会修改 PostgreSQL 密码或 secrets 卷。迁移失败时保留现场，不自动回滚数据库 schema；应先查看迁移日志，再使用与数据库版本匹配的镜像处理。

## 旧 6868 入口迁移到 Caddy

此迁移只切换 Web 的宿主机入口，不搬迁 PostgreSQL、Redis、上传文件、`.settings-key` 或 secrets 卷。迁移期间用旧 `6868` 和新 `3000` 双入口并存，先验收 Caddy，再移除旧入口。

“无缝”在这里表示数据卷、账号、登录态配置和应用内网链路不变；如果当前 1Panel 的 OpenResty 已占用宿主机 `80/443`，把网关所有权交给 Caddy 仍需要一个很短的交接窗口。只有前面还有负载均衡器、第二公网 IP 或备用节点时，才能做到网关层绝对零中断。

### 一键迁移

服务器已经安装 Caddy、Docker Compose v2，并且 Caddy 由 systemd 管理时，可以直接执行：

```bash
sudo bash 1panel-deploy/migrate-to-caddy.sh \
  --project-dir /opt/1panel/compose/open-ai-canvas \
  --domain canvas.example.com \
  --takeover-service openresty \
  --yes
```

`--project-dir` 必须是当前 1Panel 编排实际使用的目录，里面应有原 `.env` 和 Compose 文件；如果当前网关是 Docker 容器，改用 `--takeover-container <容器名或ID>`。脚本会检查现有 `web`、`backend`、PostgreSQL 和 Redis 容器，备份配置到 `/var/backups/open-ai-canvas-caddy/<UTC时间>/`，临时增加回环 `3000`，校验并启用 Caddy，完成 HTTPS 健康检查后将 `CANVAS_HTTP_PORT` 收口为 `3000`。它不会安装第二套数据库、执行 `down -v`、删除卷或停止 Backend/PostgreSQL/Redis。

如果 `/etc/caddy/Caddyfile` 已有其他站点内容，脚本默认拒绝覆盖；确认它是本应用专用配置后再追加 `--replace-caddyfile`。

如果 DNS 或证书尚未就绪，可加 `--skip-public-check` 只完成本机切换，但必须随后手工验证真实 HTTPS 域名。脚本失败会尝试恢复原 `.env`、Web 旧端口、Caddy 配置和旧网关；失败后仍应检查备份目录与服务状态。

1. 备份 PostgreSQL、后端数据目录、`.settings-key` 和 Redis AOF；从旧容器挂载信息核对四个真实卷名。保留旧私有 `.env`，不要执行 `down -v`。
2. 保持旧 `.env` 的 `CANVAS_HTTP_PORT=6868`，补齐真实的 `CANVAS_CORS_ORIGINS` 和 `CANVAS_PUBLIC_BASE_URL`。固定已经发布且前后端一致的镜像标签。
3. 在本机生成桥接编排：

   ```bash
   bun 1panel-deploy/render-editor.mjs \
     --env-file 1panel-deploy/.env \
     --image-tag sha-<已发布完整提交SHA> \
     --add-http-port 3000 \
     --output .local/1panel-deploy/docker-compose.caddy-bridge.yml
   ```

   生成文件会把四个卷标记为 `external: true`，并让 Web 同时监听旧的 `127.0.0.1:6868` 与新的 `127.0.0.1:3000`。把整份文件替换到 1Panel 编排中，不要追加到旧 YAML 末尾，也不要修改卷名或密码。
4. 在宿主机准备 `Caddyfile.example`，把域名替换为真实域名，先运行 `caddy validate`。确认 `http://127.0.0.1:3000/` 可访问后，安排网关交接：停止或移除 1Panel 站点对 `80/443` 的占用，启动并 reload Caddy。不要先停止 PostgreSQL、Redis 或 Backend。
5. 用真实 HTTPS 域名验证登录、Cookie、`/api/health/ready`、文本 SSE、资源 Range 和生成任务恢复。旧 1Panel 入口 `6868` 在这一步仍可作为回退入口。
6. 验收通过后，将私有环境中的 `CANVAS_HTTP_PORT` 改为 `3000`，重新生成**不带** `--add-http-port` 的最终编辑器编排，再整份替换 1Panel 配置并只重建 Web。确认 `6868` 不再被使用后，删除旧 1Panel 反向代理站点。

如果迁移中止，先把 Caddy 停止并恢复旧 1Panel 站点，再继续使用 `6868`；不要回滚数据库迁移，也不要删除任何卷。

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

若仍提示 `CANVAS_CORS_ORIGINS is missing a value`，说明 1Panel 仍在使用旧的 `:?` CORS 模板，错误发生在 Compose 解析阶段，尚未进入容器启动。在 1Panel 整份替换为当前 `docker-compose.yml`，同源访问可让变量留空；跨域访问再填写实际浏览器 Origin（包含协议和非默认端口）。仅在本机修改 `.env` 或推送 `.env.example` 不会同步服务器配置，编辑器模式不要将 `KEY=value` 文本追加到 YAML。直接 IP 访问还需 `CANVAS_BIND_ADDRESS=0.0.0.0`，反向代理部署按实际网络保留回环绑定。不要改为 `*` 或移除后端 CORS 校验。

外层反向代理需保留 `Host`、`X-Forwarded-Proto`、`X-Forwarded-For`，并为实际使用的 SSE 路径配置流式转发。反向代理若运行在独立容器中，其 `127.0.0.1` 不是宿主机，应使用可到达宿主机的网络与地址。

网页 Nginx 从原始 `Host` 读取完整主机名和端口，并覆盖客户端自带的 `X-Forwarded-Host`；普通 API、SSE、分享和 OAuth 回调统一处理。旧网页镜像可能丢失端口，更新前可在 `backend.environment` 配置完整 `CANVAS_CORS_ORIGINS` 并更新编排重建后端；仅修改文件或普通重启不会改变已有容器环境变量。外层反向代理也需保留公开端口。

## 旧部署数据保护

已有 `.env` 可能包含真实密码，必须沿用对应部署中验证有效的凭据，不以模板值覆盖。该文件被 Git 忽略；不要公开上传、提交仓库、贴出内容或无脱敏的 `docker compose config` 输出。在 Linux 服务器将权限限制为仅部署账号可读写（例如 `chmod 600 .env`）。仓库模板不代表已核实目标服务器的数据或凭据。

当前运行配置不保证与其他日期的整库物理备份密码一致。恢复物理数据卷时，密码必须与备份中的数据库角色匹配；恢复逻辑 dump 时，连接凭据由目标数据库决定。若已有 secrets 卷保存了不同密码，编排会明确拒绝启动，不会自动覆盖。不要删除原 secrets 卷来绕过校验，先核对数据与凭据来源。

默认卷名来自此前的编排，更新前必须在旧容器挂载信息中确认，不能把默认值当作自动识别结果。卷名不匹配会创建空卷。

| 变量 | 默认值 | 需保留的内容 |
| --- | --- | --- |
| `CANVAS_POSTGRES_VOLUME` | `open-ai-canvas_postgres-data` | PostgreSQL 17 数据 |
| `CANVAS_BACKEND_VOLUME` | `open-ai-canvas_backend-data` | 上传文件及 `.settings-key` 加密密钥 |
| `CANVAS_SECRETS_VOLUME` | `open-ai-canvas_deployment-secrets` | 数据库密码和连接串 |
| `CANVAS_REDIS_VOLUME` | `open-ai-canvas_redis-data` | Redis AOF 数据 |

- 原部署使用宿主机目录时，应按原挂载修改编排，不能把目录路径填入卷名变量。
- 旧库必须保留对应 secrets 卷，或填写匹配的原密码；不要留空尝试重新生成旧库密码。手动密码限至少 32 位字母与数字，其他格式不能直接套用此模板。改环境变量不等于修改数据库密码。
- `.settings-key` 与数据库密码不同，必须保留，否则原存储或渠道凭据可能无法解密。
- 不要将 SQLite、SQL dump 或压缩包直接挂入 PostgreSQL 数据目录，也不要跨 PostgreSQL 主版本直接挂载数据文件。
- 不要删除旧卷、执行 `docker compose down -v` 或清空 Redis AOF。仓库分发的本目录不包含私有 `.env`、数据库备份或 `.settings-key`，也不会挂载本机测试数据库。

## 更新与验收

应用镜像默认使用我们仓库的 `latest`，迁移、后端和前端均设置 `pull_policy: always`。1Panel 自己的预拉取阶段仍可能提示“使用存量镜像”；后续 Compose 才执行模板中的拉取策略，解析失败时尚未执行这一步。更新时启用面板的强制拉取选项，并检查实际拉取、迁移和容器版本结果，不能仅凭该提示判断已更新。固定共同 SHA 更容易核对；普通容器重启不拉取，也不会在运行期间自动更新。由 1Panel 管理升级，不接入 Host Updater 或 Docker socket。

只推送 `codex/*` 分支不会自动发布镜像。发布工作流在 `main`、`v*` 标签推送或手动触发时运行；手动运行开发分支可发布 SHA 标签但不更新 `latest`，`main` 发布才更新 `latest`。升级前确认前后端镜像都已成功且对应同一提交。

本次 schema 从 27 升至 29，无需新增服务或改卷名。建议固定共同的 `sha-<完整提交SHA>` 镜像标签，先备份并确认活动任务，再停止旧网页/后端，重新运行目标版本的 `migrate`；其 `up` 和 `verify` 均成功后才重建应用。依赖顺序不负责停止旧后端，普通重启不拉镜像，上次迁移成功也不能替代本次迁移。完整命令与回退边界见 [1Panel 升级说明](../docs/content/docs/backend/1panel-deployment.mdx)。

通用 `docker-compose.deploy.yml` 和上游一键部署脚本仍默认使用 `ddcat-ai`，不要用它们替换此目录的增强版编排。

Redis 启动宽限只能容纳 AOF 恢复耗时，不能修复损坏、权限或磁盘问题。迁移失败先查看 `migrate` 日志，Redis unhealthy 先查看 Redis 日志，保留原数据再处理。迁移后不能盲目回退旧应用镜像。

健康检查通过后，还需验证登录、历史资源、个人统计、存储配置解密、显示名称保存以及生成轮询与刷新恢复。
