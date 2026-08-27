# GlobalAiOpc 图片

该插件把影策图片生成合同转换为 GlobalAiOpc 的统一异步任务接口。创建和查询共用模型中心任务路径，成功后由宿主下载图片并保存为影策资源；插件不在浏览器中暴露密钥，也不绕过任务计费与资源归属校验。

## 接口与鉴权

{{OPERATIONS}}

渠道 Base URL 默认填写 `https://zcbservice.aizfw.cn/kyyReactApiServer`。创建请求使用 Bearer API Key 和 JSON 请求体；返回必须包含任务 ID。轮询成功后识别 `image_url`、`result_url` 或 `url`，缺少结果地址会作为真实失败返回。

## 模型与能力

当前静态目录包含 `seedream_5.0Pro` 与 `seedream-5.0`。模型名原样发送，不在协议层改写。`seedream_5.0Pro` 使用 `1K/2K`，其他当前图片模型使用 `2K/3K/4K`。支持比例为 `1:1`、`3:4`、`4:3`、`16:9`、`9:16`、`3:2`、`2:3`、`21:9`，每次只返回一张图片，不支持蒙版编辑。

## 参数与字段映射

{{PARAMETERS}}

请求字段包括 `model`、`prompt`、可选 `reference_images`、`aspect_ratio`、`resolution` 与 `watermark`。参考图片必须先经过影策资源服务转换为上游可访问的公网 HTTP(S) URL；Data URL、Cookie 保护链接和私网地址不会直接提交。

## 请求与结果

创建响应的 `code` 为 `0`、`ok`、`success` 等成功值时继续解析 `data`；非成功码使用上游 `msg/message` 返回错误。任务状态 `queued/pending/processing/running` 继续等待，`completed/succeeded/success` 下载结果，`failed/cancelled/canceled/expired` 终止并释放影策任务预留额度。

## 官方资料与边界

- 模型目录、分辨率和供应商额度以当前 GlobalAiOpc 账户实际返回为准。
- 插件只实现当前任务接口字段，不根据模型名称猜测价格、参考数量或额外能力。
- 管理员保存的模型能力配置是生成校验的事实来源，协议只负责按该配置发送用户选择值。

{{CONTRACT}}
