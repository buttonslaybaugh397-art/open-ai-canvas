# KBAI AI API

该目录是 KBAI AI API 的独立官方协议插件源码。插件提供视频异步任务和图片生成/编辑两个 Provider，后端通过声明式协议运行时发出请求，不依赖系统内置 `host:` 适配器。

完整字段映射见 [docs/interface.md](docs/interface.md)。

## 能力

- `kbai-video`：`POST /v1/videos` 提交视频任务，`GET /v1/videos/{task_id}` 查询结果。
- `kbai-image`：无参考图调用 `POST /v1/images/generations`，有参考图自动调用 `POST /v1/images/edits`。
- 统一使用 `Authorization: Bearer YOUR_API_KEY`，API Key 只配置在渠道中，不写入插件清单。
- 视频参考素材按 `images`、`videos`、`audios` 数组顺序对应提示词中的 `@图片1`、`@视频1`、`@音频1`。

## 配置

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | KBAI AI API Key。 |

默认 Base URL 为 `https://api.ai.kbai.cc`。如平台渠道允许覆盖 Base URL，可按部署方式配置；插件不会把 Key 拼接到 URL。

## 视频

视频请求默认使用 `aspect_ratio=9:16`、`seconds=5`、`resolution=720p`。模型、时长、分辨率和参考素材数量必须以 `/v1/models` 返回的模型 `params` 为准。视频下载地址是上游临时地址，插件将结果标记为临时媒体，由宿主负责下载和持久化。

支持的状态包括 `queued`、`running`、`succeeded`、`failed` 和 `expired`。宿主会把 `queued` 映射为等待、`running` 映射为处理中，把 `succeeded` 映射为成功，把 `failed` 和 `expired` 映射为失败，并保留 API 返回的错误说明。

## 图片

文生图请求发送 JSON：

```json
{
  "model": "your-image-model",
  "prompt": "一只橘猫躺在白色毛毯上，柔和自然光",
  "size": "1024x1024",
  "quality": "1K",
  "n": 1
}
```

图生图请求发送 multipart：`model`、`prompt`、`size`、`quality`、`n` 作为表单字段，输入图片作为 `image` 文件。协议当前只接受一张输入图，因为接口示例使用单个 `image` 文件字段；多图编辑请以模型独立文档为准，未在该插件中猜测扩展字段。

## 辅助接口与 Webhook

模型列表、价格列表、余额、模型独立文档和批量查询属于可选辅助接口，不影响本插件的提交与轮询主流程。Webhook 也是 KBAI 平台侧的可选能力：不填写 Webhook URL 时不会回调；本插件不向任务请求体中注入未在提交示例中定义的 Webhook 字段。需要回调时，应在 KBAI 管理端配置 `task.completed`、签名校验和重试策略。

Webhook 签名相关请求头为 `X-AI-Signature` 与 `X-AI-Timestamp`，签名内容为 `timestamp + "." + raw_body`。这属于客户回调服务的验签合同，不是工作台向 KBAI 发出的模型请求。

## 错误

程序应按 API 的错误分类字段判断错误类型，实际展示信息优先使用 `error.message` 或等价的 `message` 字段。常见分类包括 `unauthorized`、`invalid_api_key`、`model_not_found`、`insufficient_credits`、`task_not_found`、`rate_limited`、`video_not_ready`、`upstream_error` 和 `video_generation_failed`。

插件不保存或打印 API Key，不把错误信息降级为成功，也不会把过期的媒体 URL 当作永久地址。
