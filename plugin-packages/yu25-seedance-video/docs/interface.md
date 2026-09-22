# YU25 Seedance 视频接口

## 协议身份

- 插件 ID：`yu25-seedance-video`
- Provider ID：`yu25-seedance-video`
- API：`yingce.plugin/v2`，声明式运行时
- 能力：`video`
- 默认 Base URL：`https://api.yu25.xyz`
- 鉴权：Bearer API Key
- 公网素材：`requiresPublicMediaUrls: true`
- 结果策略：`preferResultDownload: true`，成功后优先读取鉴权的 `/content` 结果端点

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | `secret` | 是 | YU25 API Key，由宿主注入 `Authorization: Bearer ...` |

## 统一字段映射

| 统一字段 | 类型 | 上游字段 | 说明 |
| --- | --- | --- | --- |
| `model` | string | `model` | 按 YU25 公开模型名原样传递。 |
| `prompt` | string | `prompt` | 非空视频提示词。 |
| `duration` / `output.duration` | integer | `seconds` | 单个整数秒；模型范围由 YU25 校验。 |
| `resolution` / `output.resolution` | string | `resolution` | 例如 `480p`、`720p`、`768p`、`2k`。 |
| `aspectRatio` / `output.aspectRatio` | string | `aspect_ratio` | `21:9`、`16:9`、`4:3`、`1:1`、`3:4`、`9:16`。 |
| `images` | media[] | `images[]` | 取 `value`，按 `order` 排序。 |
| `videos` | media[] | `video_urls[]` | 取 `value`，按 `order` 排序。 |
| `audios` | media[] | `audio_urls[]` | 取 `value`，按 `order` 排序。 |
| `providerOptions` | object | 未默认发送 | 保留给后续 YU25 专用扩展。 |

创建 body 不发送 `duration`、`ratio`、`size`、`image_refs` 等重复别名。空素材数组会被省略。

## 上游请求模板逐字段清单

| 映射位置 | 值或转换表达式 |
| --- | --- |
| `create.method` | `POST` |
| `create.path` | `/v1/videos` |
| `create.contentType` | `application/json` |
| `create.body.model` | `request.model` |
| `create.body.prompt` | `request.prompt` |
| `create.body.seconds` | `request.output.duration`，宿主会从旧版 `request.duration` 补齐 |
| `create.body.resolution` | `request.output.resolution`，空值省略 |
| `create.body.aspect_ratio` | `request.output.aspectRatio`，空值省略 |
| `create.body.images` | `request.images` 的 `value` 数组，按 `order` 排序，空值省略 |
| `create.body.video_urls` | `request.videos` 的 `value` 数组，按 `order` 排序，空值省略 |
| `create.body.audio_urls` | `request.audios` 的 `value` 数组，按 `order` 排序，空值省略 |
| `poll.method` | `GET` |
| `poll.path` | `/v1/videos/{{taskId}}` |
| `result.method` | `GET` |
| `result.path` | `/v1/videos/{{taskId}}/content` |
| `result.headers.Accept` | `video/mp4` |

## 模型目录

参考插件 `1.5.26` 当前公开模型为：

`SD 2.0 基础`、`SD 2.0`、`SD 2.0-933`、`SD 2.5`、`SD 2.5-101010`、`SD 2.5-301010`、`seedance-2.5-pro`、`seedance-2.5-pro-480`、`sd-2-v4`、`sd2.5`、`minimax-h3 2k`、`minimax-h3 768p`、`sd2.5 高`。

模型级约束不硬编码进通用 manifest，包括：

- 固定分辨率和可选时长；
- 参考图片、视频、音频数量上限；
- Pro 模型不支持参考视频，参考音频需要参考图；
- `SD 2.5-301010` 使用音频时需要参考图或参考视频；
- 部分模型要求参考素材为 HTTPS；
- 提示词字符上限。

这样可以让 YU25 当前后台模型能力成为最终约束，避免插件静默改写用户请求。桌面版会把 4 秒转换为 5 秒的兼容行为也没有移植到影策协议。

## 响应映射

### 创建和轮询

任务 ID按以下顺序查找：

1. `request_id`
2. `id`
3. `task_id`
4. `data.request_id`
5. `data.id`
6. `data.task_id`
7. `data.data.id`
8. `data.data.task_id`
9. `video.request_id`
10. `video.id`
11. 已有的 `taskId`

状态从顶层、`data`、`result`、`data.data` 和 `video` 的 `status/state` 查找并规范化：

| 上游状态 | 影策状态 |
| --- | --- |
| `queued`、`pending`、`submitted`、`not_start`、`not_started` | `pending` |
| `processing`、`in_progress`、`running` | `processing` |
| `completed`、`complete`、`succeeded`、`success`、`done`、`finished`、`ready` | `succeeded` |
| `failed`、`failure`、`fail`、`error`、`timeout`、`expired`、`rejected` | `failed` |
| `cancelled`、`canceled` | `cancelled` |

未知状态按 `pending` 处理并继续轮询。失败信息从 `error.message`、`error.detail`、`error`、`message`、`detail`、`failure_reason`、`fail_reason` 及对应 `data` 嵌套路径查找。

### 视频地址

只有成功状态才读取视频地址，优先级为：

`video_url`、`output_url`、`download_url`、`result_url`、`url`、`data.video_url`、`data.output_url`、`data.download_url`、`data.result_url`、`data.url`、`data.data.video_url`、`data.data.output_url`、`data.data.download_url`、`data.data.url`、`result.video_url`、`result.output_url`、`result.download_url`、`result.url`、`video.video_url`、`video.download_url`、`video.url`、`output.video_url`、`output.url`、`outputs[0].video_url`、`outputs[0].url`、`videos[0].video_url`、`videos[0].url`、`data.outputs[0].video_url`、`data.outputs[0].url`、`choices[0].message.content`。

视频 URL 标记为临时结果，由影策下载后持久化。成功后宿主优先使用同一任务 ID调用鉴权的 `/v1/videos/{task_id}/content`，即使轮询响应已带 URL 也不会优先走外链；若该结果端点下载失败且轮询响应带有可下载 URL，才回退到该 URL。整个过程不会再次创建任务。

## 兼容边界

原桌面插件还负责本地参考素材压缩、图片格式转换、临时上传、轮询进度 UI、下载校验和自动更新。本影策包只实现协议运行时所需的 HTTP 创建、查询、结果优先下载/URL 回退和统一媒体映射；本地素材准备由影策公网媒体流程负责。

创建请求没有任务 ID时，宿主不会基于 502/503/504 自动重复 POST；轮询和视频下载可在已有任务 ID上重试。API Key 不进入日志、请求 body 或结果 URL。

## 响应与错误

HTTP 错误由宿主保持失败语义。业务错误字段按结构化路径读取：`error.code`、`error.type`、`error.message`、`error.detail`，以及对应的 `data.error.*`、`data.data.error.*` 路径；错误消息由上节路径映射。成功后优先使用声明的鉴权二进制 `/content` 结果操作；该操作失败时，若轮询响应有 URL，再按 URL 回退下载。空内容不会被当作成功素材。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "yu25-seedance-video",
  "name": "YU25 Seedance 视频",
  "version": "1.0.0",
  "author": "YU25 / 影策",
  "description": "面向 YU25 api.yu25.xyz 的 Seedance 异步视频协议插件。",
  "permissions": [
    "generation.run",
    "media.read"
  ],
  "configuration": {
    "fields": [
      {
        "name": "apiKey",
        "type": "secret",
        "label": "API Key",
        "required": true
      }
    ]
  },
  "contributes": {
    "providers": [
      {
        "id": "yu25-seedance-video",
        "label": "YU25 Seedance 视频",
        "capabilities": [
          "video"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation",
          "agent"
        ],
        "baseUrl": "https://api.yu25.xyz",
        "requiresPublicMediaUrls": true,
        "preferResultDownload": true,
        "auth": {
          "type": "bearer",
          "field": "apiKey"
        },
        "parameters": [
          {
            "name": "model",
            "type": "string",
            "required": true,
            "mapping": "model",
            "description": "YU25 模型名，必须按公开目录原样传递。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频提示词。"
          },
          {
            "name": "images",
            "type": "media[]",
            "required": false,
            "mapping": "images[]",
            "description": "按 order 排序的参考图片公网 URL。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "required": false,
            "mapping": "video_urls[]",
            "description": "按 order 排序的参考视频公网 URL；不支持的模型会由上游拒绝。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "required": false,
            "mapping": "audio_urls[]",
            "description": "按 order 排序的参考音频公网 URL；模型限制由上游执行。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": true,
            "mapping": "seconds",
            "description": "视频时长，按所选模型能力填写整数秒。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "required": false,
            "mapping": "aspect_ratio",
            "description": "画幅比例，例如 16:9、9:16。"
          },
          {
            "name": "resolution",
            "type": "string",
            "required": false,
            "mapping": "resolution",
            "description": "分辨率档位，例如 480p、720p、768p、2k。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "required": false,
            "mapping": "provider-specific fields",
            "description": "保留给未来 YU25 扩展字段；当前核心请求不依赖它。"
          }
        ],
        "validations": [
          {
            "assert": {
              "$and": [
                {
                  "$ne": [
                    {
                      "$trim": {
                        "$ref": "request.model"
                      }
                    },
                    ""
                  ]
                },
                {
                  "$ne": [
                    {
                      "$trim": {
                        "$ref": "request.prompt"
                      }
                    },
                    ""
                  ]
                }
              ]
            },
            "message": "YU25 Seedance 需要填写模型名和提示词"
          },
          {
            "assert": {
              "$gte": [
                {
                  "$ref": "request.output.duration"
                },
                1
              ]
            },
            "message": "YU25 Seedance 时长必须是正整数；具体范围以所选模型能力为准"
          },
          {
            "assert": {
              "$or": [
                {
                  "$eq": [
                    {
                      "$trim": {
                        "$ref": "request.output.aspectRatio"
                      }
                    },
                    ""
                  ]
                },
                {
                  "$in": [
                    {
                      "$trim": {
                        "$ref": "request.output.aspectRatio"
                      }
                    },
                    [
                      "21:9",
                      "16:9",
                      "4:3",
                      "1:1",
                      "3:4",
                      "9:16"
                    ]
                  ]
                }
              ]
            },
            "message": "YU25 Seedance 画幅只支持 21:9、16:9、4:3、1:1、3:4 或 9:16；具体模型能力以 YU25 目录为准"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/videos",
          "contentType": "application/json",
          "body": {
            "model": {
              "$ref": "request.model"
            },
            "prompt": {
              "$ref": "request.prompt"
            },
            "seconds": {
              "$ref": "request.output.duration"
            },
            "resolution": {
              "$omitEmpty": {
                "$ref": "request.output.resolution"
              }
            },
            "aspect_ratio": {
              "$omitEmpty": {
                "$ref": "request.output.aspectRatio"
              }
            },
            "images": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.images"
                    }
                  },
                  "as": "media",
                  "in": {
                    "$ref": "media.value"
                  }
                }
              }
            },
            "video_urls": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.videos"
                    }
                  },
                  "as": "media",
                  "in": {
                    "$ref": "media.value"
                  }
                }
              }
            },
            "audio_urls": {
              "$omitEmpty": {
                "$map": {
                  "from": {
                    "$sortByOrder": {
                      "$ref": "request.audios"
                    }
                  },
                  "as": "media",
                  "in": {
                    "$ref": "media.value"
                  }
                }
              }
            }
          }
        },
        "poll": {
          "method": "GET",
          "path": "/v1/videos/{{taskId}}"
        },
        "result": {
          "method": "GET",
          "path": "/v1/videos/{{taskId}}/content",
          "headers": {
            "Accept": "video/mp4"
          }
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.request_id"
              },
              {
                "$ref": "response.id"
              },
              {
                "$ref": "response.task_id"
              },
              {
                "$ref": "response.data.request_id"
              },
              {
                "$ref": "response.data.id"
              },
              {
                "$ref": "response.data.task_id"
              },
              {
                "$ref": "response.data.data.id"
              },
              {
                "$ref": "response.data.data.task_id"
              },
              {
                "$ref": "response.video.request_id"
              },
              {
                "$ref": "response.video.id"
              },
              {
                "$ref": "taskId"
              }
            ]
          },
          "status": {
            "$switch": {
              "cases": [
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$coalesce": [
                              {
                                "$ref": "response.status"
                              },
                              {
                                "$ref": "response.state"
                              },
                              {
                                "$ref": "response.data.status"
                              },
                              {
                                "$ref": "response.data.state"
                              },
                              {
                                "$ref": "response.result.status"
                              },
                              {
                                "$ref": "response.result.state"
                              },
                              {
                                "$ref": "response.data.data.status"
                              },
                              {
                                "$ref": "response.data.data.state"
                              },
                              {
                                "$ref": "response.video.status"
                              },
                              {
                                "$ref": "response.video.state"
                              }
                            ]
                          }
                        }
                      },
                      [
                        "queued",
                        "pending",
                        "submitted",
                        "not_start",
                        "not_started"
                      ]
                    ]
                  },
                  "then": "pending"
                },
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$coalesce": [
                              {
                                "$ref": "response.status"
                              },
                              {
                                "$ref": "response.state"
                              },
                              {
                                "$ref": "response.data.status"
                              },
                              {
                                "$ref": "response.data.state"
                              },
                              {
                                "$ref": "response.result.status"
                              },
                              {
                                "$ref": "response.result.state"
                              },
                              {
                                "$ref": "response.data.data.status"
                              },
                              {
                                "$ref": "response.data.data.state"
                              },
                              {
                                "$ref": "response.video.status"
                              },
                              {
                                "$ref": "response.video.state"
                              }
                            ]
                          }
                        }
                      },
                      [
                        "processing",
                        "in_progress",
                        "running"
                      ]
                    ]
                  },
                  "then": "processing"
                },
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$coalesce": [
                              {
                                "$ref": "response.status"
                              },
                              {
                                "$ref": "response.state"
                              },
                              {
                                "$ref": "response.data.status"
                              },
                              {
                                "$ref": "response.data.state"
                              },
                              {
                                "$ref": "response.result.status"
                              },
                              {
                                "$ref": "response.result.state"
                              },
                              {
                                "$ref": "response.data.data.status"
                              },
                              {
                                "$ref": "response.data.data.state"
                              },
                              {
                                "$ref": "response.video.status"
                              },
                              {
                                "$ref": "response.video.state"
                              }
                            ]
                          }
                        }
                      },
                      [
                        "completed",
                        "complete",
                        "succeeded",
                        "success",
                        "done",
                        "finished",
                        "ready"
                      ]
                    ]
                  },
                  "then": "succeeded"
                },
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$coalesce": [
                              {
                                "$ref": "response.status"
                              },
                              {
                                "$ref": "response.state"
                              },
                              {
                                "$ref": "response.data.status"
                              },
                              {
                                "$ref": "response.data.state"
                              },
                              {
                                "$ref": "response.result.status"
                              },
                              {
                                "$ref": "response.result.state"
                              },
                              {
                                "$ref": "response.data.data.status"
                              },
                              {
                                "$ref": "response.data.data.state"
                              },
                              {
                                "$ref": "response.video.status"
                              },
                              {
                                "$ref": "response.video.state"
                              }
                            ]
                          }
                        }
                      },
                      [
                        "failed",
                        "failure",
                        "fail",
                        "error",
                        "timeout",
                        "expired",
                        "rejected"
                      ]
                    ]
                  },
                  "then": "failed"
                },
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$coalesce": [
                              {
                                "$ref": "response.status"
                              },
                              {
                                "$ref": "response.state"
                              },
                              {
                                "$ref": "response.data.status"
                              },
                              {
                                "$ref": "response.data.state"
                              },
                              {
                                "$ref": "response.result.status"
                              },
                              {
                                "$ref": "response.result.state"
                              },
                              {
                                "$ref": "response.data.data.status"
                              },
                              {
                                "$ref": "response.data.data.state"
                              },
                              {
                                "$ref": "response.video.status"
                              },
                              {
                                "$ref": "response.video.state"
                              }
                            ]
                          }
                        }
                      },
                      [
                        "cancelled",
                        "canceled"
                      ]
                    ]
                  },
                  "then": "cancelled"
                }
              ],
              "default": "pending"
            }
          },
          "message": {
            "$coalesce": [
              {
                "$ref": "response.error.message"
              },
              {
                "$ref": "response.error.detail"
              },
              {
                "$ref": "response.error"
              },
              {
                "$ref": "response.message"
              },
              {
                "$ref": "response.detail"
              },
              {
                "$ref": "response.fail_reason"
              },
              {
                "$ref": "response.failure_reason"
              },
              {
                "$ref": "response.data.error.message"
              },
              {
                "$ref": "response.data.error"
              },
              {
                "$ref": "response.data.message"
              },
              {
                "$ref": "response.data.data.error.message"
              },
              {
                "$ref": "response.data.data.error"
              },
              {
                "$ref": "response.data.data.message"
              }
            ]
          },
          "videos": {
            "$if": {
              "condition": {
                "$eq": [
                  {
                    "$switch": {
                      "cases": [
                        {
                          "when": {
                            "$in": [
                              {
                                "$lower": {
                                  "$trim": {
                                    "$coalesce": [
                                      {
                                        "$ref": "response.status"
                                      },
                                      {
                                        "$ref": "response.state"
                                      },
                                      {
                                        "$ref": "response.data.status"
                                      },
                                      {
                                        "$ref": "response.data.state"
                                      },
                                      {
                                        "$ref": "response.result.status"
                                      },
                                      {
                                        "$ref": "response.result.state"
                                      },
                                      {
                                        "$ref": "response.data.data.status"
                                      },
                                      {
                                        "$ref": "response.data.data.state"
                                      },
                                      {
                                        "$ref": "response.video.status"
                                      },
                                      {
                                        "$ref": "response.video.state"
                                      }
                                    ]
                                  }
                                }
                              },
                              [
                                "completed",
                                "complete",
                                "succeeded",
                                "success",
                                "done",
                                "finished",
                                "ready"
                              ]
                            ]
                          },
                          "then": "succeeded"
                        }
                      ],
                      "default": "pending"
                    }
                  },
                  "succeeded"
                ]
              },
              "then": {
                "$coalesce": [
                  {
                    "$ref": "response.video_url"
                  },
                  {
                    "$ref": "response.output_url"
                  },
                  {
                    "$ref": "response.download_url"
                  },
                  {
                    "$ref": "response.result_url"
                  },
                  {
                    "$ref": "response.url"
                  },
                  {
                    "$ref": "response.data.video_url"
                  },
                  {
                    "$ref": "response.data.output_url"
                  },
                  {
                    "$ref": "response.data.download_url"
                  },
                  {
                    "$ref": "response.data.result_url"
                  },
                  {
                    "$ref": "response.data.url"
                  },
                  {
                    "$ref": "response.data.data.video_url"
                  },
                  {
                    "$ref": "response.data.data.output_url"
                  },
                  {
                    "$ref": "response.data.data.download_url"
                  },
                  {
                    "$ref": "response.data.data.url"
                  },
                  {
                    "$ref": "response.result.video_url"
                  },
                  {
                    "$ref": "response.result.output_url"
                  },
                  {
                    "$ref": "response.result.download_url"
                  },
                  {
                    "$ref": "response.result.url"
                  },
                  {
                    "$ref": "response.video.video_url"
                  },
                  {
                    "$ref": "response.video.download_url"
                  },
                  {
                    "$ref": "response.video.url"
                  },
                  {
                    "$ref": "response.output.video_url"
                  },
                  {
                    "$ref": "response.output.url"
                  },
                  {
                    "$ref": "response.outputs.0.video_url"
                  },
                  {
                    "$ref": "response.outputs.0.url"
                  },
                  {
                    "$ref": "response.videos.0.video_url"
                  },
                  {
                    "$ref": "response.videos.0.url"
                  },
                  {
                    "$ref": "response.data.outputs.0.video_url"
                  },
                  {
                    "$ref": "response.data.outputs.0.url"
                  },
                  {
                    "$ref": "response.choices.0.message.content"
                  }
                ]
              },
              "else": null
            }
          },
          "errorPaths": [
            "error.code",
            "error.type",
            "error.message",
            "error.detail",
            "data.error.code",
            "data.error.type",
            "data.error.message",
            "data.error.detail",
            "data.data.error.code",
            "data.data.error.type",
            "data.data.error.message",
            "data.data.error.detail"
          ],
          "resultKind": "video",
          "resultEphemeral": true,
          "messagePaths": [
            "error.message",
            "error.detail",
            "error",
            "message",
            "detail",
            "failure_reason",
            "fail_reason",
            "data.error.message",
            "data.error",
            "data.message",
            "data.data.error.message",
            "data.data.error",
            "data.data.message"
          ]
        }
      }
    ]
  },
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>"
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
