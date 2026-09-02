# 维今 ONE 视频

该插件把维今 ONE 视频生成 API 接入影策统一任务系统。它使用官方动态模型目录，不在应用中写死模型名称、时长、画幅或参考素材数量。渠道默认地址为 `https://www.weijinapi.top`，API Key 仅由后端读取并以 `Authorization: Bearer <API_KEY>` 发送。

## 接口

{{OPERATIONS}}

创建任务调用 `POST /v1/videos`，查询任务调用 `GET /v1/videos/{task_id}`。模型目录由 `GET /v1/models` 获取，最终媒体由响应 URL 或 `GET /v1/videos/{task_id}/content` 下载。创建、查询和同源下载都使用同一 Bearer 密钥；结果地址不会暴露密钥，也不会把密钥拼进查询参数。影策按官方建议每 10 秒查询一次任务，创建请求可能等待较久时使用分钟级上游超时，不会按普通网页请求的短超时提前中断。

## 模型

维今可用模型随 API Key 和上游配置变化，插件不内置静态列表。后台拉取模型时读取每个模型的 `id`、`resolution`、`durations_seconds`、`ratios`、`max_images`、`max_videos`、`max_video_duration_seconds`、`max_audios` 和 `audio_requires_image`。这些值会转换为影策模型能力，用于限制画幅、时长、分辨率以及图片、视频、音频参考数量。固定分辨率通常已经编码在模型 ID 中；只有目录明确提供 `resolution` 时才作为可选能力保存。管理员仍需为新拉取模型设置价格并启用，动态发现不能绕过计费边界。

## 参数

{{PARAMETERS}}

影策的 `duration` 映射到官方 `seconds`，不能发送历史字段 `duration_seconds`；`aspectRatio` 映射到 `aspect_ratio`，不能发送旧字段 `size`。`images`、`videos`、`audios` 分别是公开 HTTPS URL 数组，空数组必须省略。模型 ID 已包含 `480p`、`720p`、`1080p`、`2160p` 或 `4k` 时，分辨率已由模型固定，请求必须省略 `resolution`；只有未来不带固定清晰度的动态模型才发送用户选择的真实档位，`auto` 和空值始终省略。参考媒体会先进入影策的公网 URL 转换流程，登录 Cookie、本机地址、裸 Base64 和浏览器临时对象 URL 都不能直接交给上游。

创建任务前，后端会把参考图片、视频和音频上传到官方 `POST /api/upload/video`，再将响应中的 HTTPS 地址提交给生成接口。上传字段固定为 `file`，单文件上限为 50 MB。恢复已有上游任务时不会重复上传素材或重新创建任务。

## 状态与结果

状态映射严格采用官方值：`queued` 为排队，`in_progress` 为处理中，`completed` 为成功，`failed` 为失败。未知状态不会猜成成功。失败消息优先读取 `error.message`，即使失败响应残留 `result_url`、`video_url`、`url` 或 `content`，插件也不会下载或返回这些地址。成功响应按顺序识别上述四个结果字段；若 `completed` 没有任何结果地址，则返回协议错误，避免产生“任务成功但画布无素材”的假成功。最终视频地址是临时或鉴权资源，影策成功后立即带同源 Bearer 鉴权下载并保存为平台资源。

## 官方

- 官方文档：<https://www.weijinapi.top/docs/>
- 官方服务根地址：<https://www.weijinapi.top>
- 鉴权方式：`Authorization: Bearer YOUR_API_KEY`
- 创建字段以当前文档中的 `seconds`、`aspect_ratio` 和三类媒体数组为准；上游新增字段不会自动进入影策请求。

## 影策运行时合同

- 插件负责请求字段转换、任务 ID 提取、严格状态映射和结果地址识别；任务租约、恢复、计费和资源持久化由影策宿主负责。
- Base URL 可填写服务根地址或带 `/v1` 的 API 根，URL 拼接会去重版本段；不要把 `/videos` 任务路径写入 Base URL。
- 密钥只用于后端创建、查询和同源结果下载，不写入浏览器 URL、插件文档、任务正文或公开日志。
- 模型目录是当前密钥的真实能力来源。目录未声明的时长、画幅、分辨率和参考数量不应由模型名称猜测。
- 停用 `weijin-channel` 会使 `weijin-video` 不再可选，已保存的模型配置也不能继续发起新任务。
- HTTP 非 2xx、无效 JSON、缺少任务 ID、未知状态、失败状态以及成功但无结果 URL 都会作为真实错误返回。
