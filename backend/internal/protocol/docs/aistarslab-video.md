# AIStarsLab 视频

该插件负责 AIStarsLab 线路化视频任务的创建、状态查询和结果下载。目录中同一视频模型可以有多个渠道号，影策把每条线路作为独立供应配置保存，避免前端只显示模型名而请求丢失渠道号。

## 接口与鉴权

{{OPERATIONS}}

渠道 Base URL 默认填写 `https://api.video.aistarslab.com/openapi`。创建路径为 `/generation/create/video`，轮询使用 `/generation/status?taskId={task_id}`，Bearer API Key 只由后端读取。任务成功后宿主立即下载视频，避免临时结果 URL 过期。

## 模型与能力

模型、线路编码、时长范围、比例、质量、模式和参考媒体上限来自 AIStarsLab 动态目录。能力配置保存 `channel`、`model`、`duration` 或连续时长范围，以及 `inputImagesMax/inputVideosMax/inputAudiosMax`。管理员保存的线路能力是唯一生成事实来源。

## 参数与字段映射

{{PARAMETERS}}

创建字段包含 `channel`、`model`、`prompt`、时长、比例、质量、生成模式，以及图片、视频和音频参考 URL。插件按目录支持项选择字段，不把 720p、480p 或固定时长写死到全部模型。缺少线路编码时停止请求，不能发送空渠道号后再把上游 400 包装成通用错误。

## 请求与结果

创建响应读取 `taskId/id`，轮询状态转换为等待、成功或失败。成功结果从 `outputs` 字符串数组、`output` 或兼容字段读取；失败原因优先使用 `errorMessage/errorCode/msg`。参考媒体在任务创建前完成公网 URL 转换和后台数量校验。

## 官方资料与边界

- 同名模型的多个线路必须分别保留渠道号、能力和价格，不能按模型名去重覆盖。
- 插件只发送目录和后台已经声明的参数；供应商新增字段需要补映射和回归测试。
- 停用 AIStarsLab 渠道插件会同时停用图片、视频两个 Provider，防止半启用状态。

{{CONTRACT}}
