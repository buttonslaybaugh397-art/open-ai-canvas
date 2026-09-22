# YU25 Seedance 视频协议插件

这是一个可安装到影策的声明式视频协议插件，基于 YU25 Seedance 视频插件 `1.5.26` 和配套 API 文档实现。

- 插件 ID：`yu25-seedance-video`
- Provider ID：`yu25-seedance-video`
- Base URL：`https://api.yu25.xyz`
- 鉴权：`Authorization: Bearer <API Key>`
- 创建：`POST /v1/videos`
- 查询：`GET /v1/videos/{task_id}`
- 结果回退下载：`GET /v1/videos/{task_id}/content`

## 安装和配置

在影策插件管理中安装 `yu25-seedance-video.yingce-plugin`，为渠道填写 YU25 API Key，然后选择 YU25 模型并发起视频生成。

插件使用影策的统一视频请求字段，模型名保持 YU25 公开目录原样，例如：

- `SD 2.0 基础`
- `SD 2.0`
- `SD 2.0-933`
- `SD 2.5`
- `SD 2.5-101010`
- `SD 2.5-301010`
- `seedance-2.5-pro`
- `seedance-2.5-pro-480`
- `sd-2-v4`
- `sd2.5`
- `minimax-h3 2k`
- `minimax-h3 768p`
- `sd2.5 高`

## 素材边界

该插件声明 `requiresPublicMediaUrls: true`。影策会把参考图片、参考视频和参考音频作为公网 URL 传给 YU25；URL 必须能被 YU25 服务端直接访问，不能是本地路径、`localhost`、内网地址或依赖浏览器登录 Cookie 的地址。

桌面版 `1.5.26` 会调用 YU25 的 `/v1/temp-images`、`/v1/temp-videos`、`/v1/temp-audios` 自动上传本地文件。影策插件不复制这段桌面 UI/上传器逻辑，素材应先由影策保存到已配置的公网对象存储，再进入协议请求。

## 能力和错误语义

插件只做非空提示词、正整数时长和通用六种画幅的基础校验。各模型的固定分辨率、时长范围、参考素材数量、参考音频前置条件和提示词长度仍由 YU25 当前模型能力及上游接口校验；插件不会静默替换模型、截断提示词或把 4 秒改成 5 秒。

视频时长是必填项；宿主会把旧版顶层 `duration` 投影到 `output.duration`，清单校验和请求映射统一使用该字段。

创建请求只提交一套字段：`model`、`prompt`、`seconds`、`resolution`、`aspect_ratio`，以及存在时的 `images`、`video_urls`、`audio_urls`。任务没有 ID 时不会自动重复创建；已有任务的轮询和结果下载沿用同一个任务 ID。

完整字段、状态和响应路径见 [docs/interface.md](docs/interface.md)。

## 安全说明

API Key 由影策渠道配置保存并由后端注入 Bearer 请求头，不写入视频提示词、URL 或插件文档。安装包不包含密钥，也不会执行桌面版 Python 代码。
