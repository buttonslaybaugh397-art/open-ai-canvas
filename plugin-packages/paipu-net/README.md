# 牌谱 API 渠道插件

基地址：`https://api.paipu.net`

插件使用牌谱 New API 公开接口：

- 文本：`POST /v1/chat/completions`
- 图片：`POST /v1/images/generations`
- 视频创建：`POST /v1/videos`
- 视频查询：`GET /v1/videos/{task_id}`

安装后在渠道设置中填写 API Key，并按牌谱 `/v1/models` 返回的公开模型 ID 配置模型。图片和视频请求会根据具体模型能力组装字段，模型专属参数和默认值通过 `providerOptions.paipu-net-image` / `providerOptions.paipu-net-video` 覆盖。

空的 `images`、`videos`、`audios` 会从请求中省略。未知状态、失败状态，以及已完成但没有结果的响应都会直接失败，不会无限轮询。连接测试会真实创建后端任务并使用对应模型的默认参数。

图片结果支持 URL 和 Base64；视频查询会读取 `url`、`result_url`、`data.0.url` 或 `metadata.url` 等结果路径。
