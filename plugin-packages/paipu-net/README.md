# 牌谱 API 渠道插件

基地址：`https://api.paipu.net`

插件使用牌谱 New API 公开接口：

- 文本：`POST /v1/chat/completions`
- 图片：`POST /v1/images/generations`
- 视频创建：`POST /v1/videos`
- 视频查询：`GET /v1/videos/{task_id}`

安装后在渠道设置中填写 API Key，并按牌谱 `/v1/models` 返回的公开模型 ID 配置模型。

视频查询只有 `status=completed` 且返回 `url`、`result_url` 或 `metadata.url` 时才会交付结果；`queued`、`in_progress`、`RESULT_STORAGE_WAIT` 等状态继续轮询。
