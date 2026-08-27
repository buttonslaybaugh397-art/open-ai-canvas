# AIStarsLab 图片

该插件把影策图片任务映射到 AIStarsLab 的线路化生成接口。每个目录模型同时保存线路编码、官方模型名和图片能力；生成时必须携带对应线路，不能只发送展示模型名或临时猜测渠道号。

## 接口与鉴权

{{OPERATIONS}}

渠道 Base URL 默认填写 `https://api.video.aistarslab.com/openapi`。创建路径为 `/generation/create/image`，轮询使用 `/generation/status?taskId={task_id}`，请求通过 Bearer API Key 鉴权。成功结果从官方输出字符串数组中读取并由宿主下载保存。

## 模型与能力

模型目录由 AIStarsLab 渠道接口动态返回，同一模型可能对应多个线路编码。影策把线路编码保存在能力配置的 `aistarslab.channel`，把官方模型名保存在 `aistarslab.model`。存量 `<线路>:<模型>` 记录可回填线路，但新拉取模型应使用结构化线路配置。

## 参数与字段映射

{{PARAMETERS}}

创建字段包含 `channel`、`model`、`prompt`、`aspectRatio`、`quality`、`inputImages` 和 `n: 1`。比例、质量与最大参考图数量来自目录能力。参考图必须为公网 URL，不支持蒙版编辑、透明背景和多张输出；缺少线路编码时明确提示管理员重新拉取模型。

## 请求与结果

创建响应读取 `taskId/id`。轮询状态码由 AIStarsLab 合同转换为等待、成功或失败；成功时读取 `outputs` 字符串数组或兼容输出字段，失败时保留 `errorMessage/errorCode/msg`。成功状态没有输出地址会作为协议错误返回。

## 官方资料与边界

- 线路、模型、比例、质量和参考数量以 AIStarsLab 当前目录返回为准。
- 同名模型的不同线路不能合并为一个无线路模型，否则会再次出现“缺少线路编码”。
- 插件停用后目录拉取和新任务创建同时停止，历史任务恢复仍由宿主任务记录控制。

{{CONTRACT}}
