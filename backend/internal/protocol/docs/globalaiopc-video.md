# GlobalAiOpc 视频

该插件使用 GlobalAiOpc 模型中心的异步任务接口生成视频，覆盖文本、参考图片、首尾帧、参考视频和参考音频。任务创建、轮询、结果下载、恢复和计费仍由影策宿主统一管理，插件只承担渠道协议映射。

## 接口与鉴权

{{OPERATIONS}}

渠道 Base URL 默认填写 `https://zcbservice.aizfw.cn/kyyReactApiServer`，请求使用 Bearer API Key。创建响应必须返回 `id`、`task_id` 或 `request_id`；查询路径携带任务 ID。成功结果从 `video_url`、`result_url` 或 `url` 读取并立即下载保存。

## 模型与能力

静态目录包括 Seedance 2.5、Seedance 1.5、SD 2.0、MiniMax H3 和 933 系列。模型名原样发送。Seedance 1.5 使用 `size` 和 `first_image/last_image`，不发送独立 `resolution`；其他模型使用 `aspect_ratio` 与 `resolution`。`MiniMax-H3-c4` 固定映射 `1440P`，`sd_2.0_special` 的 2160p 映射为上游 `4k`。

## 参数与字段映射

{{PARAMETERS}}

通用字段包括 `model`、`prompt`、`duration`、`aspect_ratio`、`resolution`、`generate_audio` 和 `watermark`。普通参考图发送 `reference_images`，首尾帧节点发送 `first_image/last_image`，参考视频和音频分别发送 `reference_videos/reference_audios`。480p、720p 等分辨率按用户和后台能力配置原样归一，不再强制覆盖为 720p。

## 请求与结果

所有参考媒体必须是公网 HTTP(S) URL。任务状态 `queued/pending/processing/running` 继续轮询，`completed/succeeded/success` 下载视频，`failed/cancelled/canceled/expired` 返回上游错误。未知状态不会被当成成功，也不会静默重建任务。

## 官方资料与边界

- 各模型支持的时长、比例、分辨率、音频和参考数量以后台保存的模型能力为准。
- 模型名中带分辨率不代表插件可以忽略后台选择；只有明确写入适配规则的固定模型才使用固定映射。
- 供应商任务恢复复用原任务 ID，不在轮询失败时重复扣费创建新任务。

{{CONTRACT}}
