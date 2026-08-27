# 汇取云视频

该插件承载汇取云专用视频生成协议。普通模型使用 JSON 创建任务，MX933 等需要参考素材的模型使用 multipart/form-data；两种路径共享同一插件开关、任务恢复、轮询和结果保存边界。

## 接口与鉴权

{{OPERATIONS}}

渠道 Base URL 默认填写 `https://api.bjhuiqu.net/v1`，创建路径为 `/videos/generations`，轮询路径为 `/videos/{task_id}`。请求使用 Bearer API Key。成功后优先读取上游视频 URL；没有 URL 时读取 `/videos/{task_id}/content`，避免把完成状态误判为无结果。

## 模型与能力

汇取云模型目录仍从渠道 `/models` 拉取，文本、图片和音频模型分别使用通用 Chat、Images 和 Audio 插件；只有视频模型选择本插件。MX933、MJ933 和固定时长模型由现有模型识别规则处理，实际时长、比例、清晰度和参考数量以后台模型能力配置为准。

## 参数与字段映射

{{PARAMETERS}}

JSON 请求包含 `model`、`prompt`、`seconds`、`resolution`、`aspect_ratio`、`audio` 以及参考图片、视频、音频 URL。需要文件上传的 MX933 请求改用 multipart，并保留已验证的字段名和时长规则。后台选择 480p 时发送 480p，选择 720p 时发送 720p；不再由通用视频默认值强行改写。

## 请求与结果

创建响应从顶层或 `data` 中读取 `id/task_id/request_id`。`queued/in_progress/processing` 继续等待，`completed/succeeded` 下载结果，`failed/cancelled/canceled` 返回上游错误。参考媒体先由影策资源服务转换为公网 URL，不能访问的素材在创建任务前失败。

## 官方资料与边界

- 汇取云不同模型可能共用 OpenAI 风格目录但使用不同视频任务合同，不能仅按模型名称猜测接口。
- 固定时长、multipart 和 MX933 规则属于宿主已验证实现，停用插件后全部协议同时不可选。
- 价格和参考数量只读取后台配置；协议层不添加九图等额外硬限制。

{{CONTRACT}}
