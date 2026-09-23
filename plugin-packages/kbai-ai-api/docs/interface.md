# KBAI AI API 接口字段

## 协议身份

- 插件 ID：`kbai-ai-api`。
- Provider：`kbai-video`、`kbai-image`。
- 默认 Base URL：`https://api.ai.kbai.cc`。
- 鉴权：`Authorization: Bearer YOUR_API_KEY`。
- API Key 配置字段：`apiKey`。

## 视频接口

### 提交任务

- 方法：`POST /v1/videos`。
- 内容类型：`application/json`。
- `model` 和 `prompt` 必填。
- `aspect_ratio` 默认 `9:16`。
- `seconds` 默认 `5`。
- `resolution` 默认 `720p`。
- 参考图片映射到 `reference_image_urls`，参考视频映射到 `reference_videos`，参考音频映射到 `reference_audios`。
- 参考 URL 由宿主按 `requiresPublicMediaUrls` 转换为公网可访问的短期地址。

### 查询任务

- 方法：`GET /v1/videos/{{taskId}}`。
- 任务 ID 优先读取 `task_id`，其次读取 `id`、`data.task_id`、`data.id`。
- 成功结果优先读取 `video_url`，同时兼容 `download_url`、`url`、`original_video_url` 及 `data` 中的同名字段。

### 状态

| KBAI 状态 | 宿主状态 | 说明 |
| --- | --- | --- |
| `queued` | `pending` | 等待平台提交或上游排队。 |
| `running` | `processing` | 已提交或正在生成。 |
| `succeeded` | `succeeded` | 返回视频下载地址。 |
| `failed` | `failed` | 保留平台和上游失败原因。 |
| `expired` | `failed` | 媒体已经过期，任务记录仍可保留。 |

## 图片接口

### 文生图

- 方法：`POST /v1/images/generations`。
- 内容类型：`application/json`。
- 字段：`model`、`prompt`、`size`、`quality`、`n`。
- 图片比例会转换为常用尺寸：`1:1 → 1024x1024`、`3:4 → 1024x1536`、`4:3 → 1536x1024`、`16:9 → 1536x864`、`9:16 → 864x1536`。
- 传入 `WxH` 尺寸时原样发送；也可通过 `providerOptions.kbai-image.size` 覆盖。

### 图生图

- 方法：`POST /v1/images/edits`。
- 内容类型：`multipart/form-data`。
- 表单字段：`model`、`prompt`、`size`、`quality`、`n`。
- 文件字段：`image`，输入源图以 `input.png` 文件名发送。
- 当前声明式协议最多接收一张输入图片；宿主负责读取 URL 或 Data URL 并写入 multipart 文件。

### 图片响应

图片 URL 读取 `data[].url`、`data[].image_url` 或 `data[].imageUrl`；内联结果读取 `data[].b64_json`，并按 `mime_type` 生成 Data URL。顶层单对象响应兼容 `image_url`、`url` 和 `b64_json`。

## 错误映射

失败响应会读取 `error_message`、`error`、`api_error.message`、`platform_message`、`upstream_error.message`、`message` 或 `msg`。`error`、`api_error`、`upstream_error`、`platform_error_code`、`code` 为失败信号路径；宿主不会把 HTTP 或业务失败包装成成功。

## 任务与下载地址

视频提交成功后先获得 `task_id`，再轮询查询接口。`video_url`、`url`、`download_url`、`original_video_url` 都被视为原始视频地址，并标记为临时结果，由宿主立即下载保存。上游直链的默认寿命约为两小时，实际由 KBAI 渠道决定。

模型列表、价格、余额、模型文档、批量查询和 Webhook 配置是平台侧辅助能力，不作为本插件的额外请求字段。Webhook 验签请求头和回调重试由 KBAI 回调服务处理，本插件不擅自向提交请求增加 `webhook_url`。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "kbai-ai-api",
  "name": "KBAI AI API",
  "version": "1.0.0",
  "author": "KBAI / 影策",
  "description": "KBAI AI API 的视频异步任务与图片生成协议插件。",
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
        "required": true,
        "description": "KBAI AI API 的 Bearer API Key。"
      }
    ]
  },
  "contributes": {
    "providers": [
      {
        "id": "kbai-video",
        "label": "KBAI AI 视频",
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
        "baseUrl": "https://api.ai.kbai.cc",
        "requiresPublicMediaUrls": true,
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
            "description": "公开视频模型 ID，以模型列表接口返回值为准。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频生成提示词；素材占位符按图片、视频、音频数组顺序对应。"
          },
          {
            "name": "images",
            "type": "media[]",
            "mapping": "reference_image_urls",
            "description": "参考图片，转换为公网 URL 数组。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "mapping": "reference_videos",
            "description": "参考视频，转换为公网 URL 数组。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "mapping": "reference_audios",
            "description": "参考音频，转换为公网 URL 数组。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "mapping": "aspect_ratio",
            "description": "画幅比例，默认 9:16；具体可用值以模型列表 params 为准。"
          },
          {
            "name": "duration",
            "type": "integer",
            "mapping": "seconds",
            "description": "视频时长，单位秒；默认 5。"
          },
          {
            "name": "resolution",
            "type": "string",
            "mapping": "resolution",
            "description": "分辨率档位，默认 720p；具体可用值以模型列表 params 为准。"
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
            "aspect_ratio": {
              "$coalesce": [
                {
                  "$ref": "request.aspectRatio"
                },
                "9:16"
              ]
            },
            "seconds": {
              "$coalesce": [
                {
                  "$ref": "request.duration"
                },
                5
              ]
            },
            "resolution": {
              "$coalesce": [
                {
                  "$ref": "request.resolution"
                },
                "720p"
              ]
            },
            "reference_image_urls": {
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
            "reference_videos": {
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
            "reference_audios": {
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
          "path": "/v1/videos/{{taskId}}",
          "contentType": "application/json"
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.task_id"
              },
              {
                "$ref": "response.id"
              },
              {
                "$ref": "response.data.task_id"
              },
              {
                "$ref": "response.data.id"
              },
              {
                "$ref": "taskId"
              }
            ]
          },
          "status": {
            "$coalesce": [
              {
                "$ref": "response.status"
              },
              {
                "$ref": "response.data.status"
              },
              "queued"
            ]
          },
          "message": {
            "$coalesce": [
              {
                "$ref": "response.error_message"
              },
              {
                "$ref": "response.error.message"
              },
              {
                "$ref": "response.api_error.message"
              },
              {
                "$ref": "response.upstream_error.message"
              },
              {
                "$ref": "response.platform_message"
              },
              {
                "$ref": "response.message"
              },
              {
                "$ref": "response.error"
              }
            ]
          },
          "videos": {
            "$if": {
              "condition": {
                "$coalesce": [
                  {
                    "$ref": "response.video_url"
                  },
                  {
                    "$ref": "response.download_url"
                  },
                  {
                    "$ref": "response.url"
                  },
                  {
                    "$ref": "response.original_video_url"
                  },
                  {
                    "$ref": "response.data.video_url"
                  },
                  {
                    "$ref": "response.data.download_url"
                  },
                  {
                    "$ref": "response.data.url"
                  }
                ]
              },
              "then": [
                {
                  "url": {
                    "$coalesce": [
                      {
                        "$ref": "response.video_url"
                      },
                      {
                        "$ref": "response.download_url"
                      },
                      {
                        "$ref": "response.url"
                      },
                      {
                        "$ref": "response.original_video_url"
                      },
                      {
                        "$ref": "response.data.video_url"
                      },
                      {
                        "$ref": "response.data.download_url"
                      },
                      {
                        "$ref": "response.data.url"
                      }
                    ]
                  }
                }
              ],
              "else": null
            }
          },
          "errorPaths": [
            "error",
            "api_error",
            "upstream_error",
            "platform_error_code",
            "code"
          ],
          "messagePaths": [
            "error_message",
            "error.message",
            "api_error.message",
            "upstream_error.message",
            "platform_message",
            "message",
            "error"
          ],
          "resultEphemeral": true
        }
      },
      {
        "id": "kbai-image",
        "label": "KBAI AI 图片",
        "capabilities": [
          "image"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation",
          "agent"
        ],
        "baseUrl": "https://api.ai.kbai.cc",
        "requiresPublicMediaUrls": false,
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
            "description": "公开图片模型 ID，以模型列表接口返回值为准。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "图片生成或编辑提示词。"
          },
          {
            "name": "images",
            "type": "media[]",
            "mapping": "image multipart file",
            "description": "可选图生图源图；当前接口示例为单文件，最多传一张。"
          },
          {
            "name": "aspectRatio",
            "type": "string",
            "mapping": "size",
            "description": "可填写上游尺寸如 1024x1024，也支持常用比例映射。"
          },
          {
            "name": "quality",
            "type": "string",
            "mapping": "quality",
            "description": "质量档位，默认 1K；具体可用值以模型列表 params 为准。"
          },
          {
            "name": "imageCount",
            "type": "integer",
            "mapping": "n",
            "description": "生成数量，默认 1。"
          },
          {
            "name": "providerOptions",
            "type": "object",
            "mapping": "provider-specific fields",
            "description": "使用 providerOptions.kbai-image.size 或 quality 覆盖图片请求字段。"
          }
        ],
        "validations": [
          {
            "assert": {
              "$lte": [
                {
                  "$len": {
                    "$ref": "request.images"
                  }
                },
                1
              ]
            },
            "message": "KBAI AI 图生图当前只支持一张输入图片"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/images/generations",
          "pathTemplate": {
            "$if": {
              "condition": {
                "$gt": [
                  {
                    "$len": {
                      "$ref": "request.images"
                    }
                  },
                  0
                ]
              },
              "then": "/v1/images/edits",
              "else": "/v1/images/generations"
            }
          },
          "contentType": "application/json",
          "contentTypeTemplate": {
            "$if": {
              "condition": {
                "$gt": [
                  {
                    "$len": {
                      "$ref": "request.images"
                    }
                  },
                  0
                ]
              },
              "then": "multipart/form-data",
              "else": "application/json"
            }
          },
          "body": {
            "model": {
              "$ref": "request.model"
            },
            "prompt": {
              "$ref": "request.prompt"
            },
            "size": {
              "$coalesce": [
                {
                  "$ref": "request.providerOptions.kbai-image.size"
                },
                {
                  "$switch": {
                    "cases": [
                      {
                        "when": {
                          "$eq": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.aspectRatio"
                                }
                              }
                            },
                            "1:1"
                          ]
                        },
                        "then": "1024x1024"
                      },
                      {
                        "when": {
                          "$eq": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.aspectRatio"
                                }
                              }
                            },
                            "3:4"
                          ]
                        },
                        "then": "1024x1536"
                      },
                      {
                        "when": {
                          "$eq": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.aspectRatio"
                                }
                              }
                            },
                            "4:3"
                          ]
                        },
                        "then": "1536x1024"
                      },
                      {
                        "when": {
                          "$eq": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.aspectRatio"
                                }
                              }
                            },
                            "16:9"
                          ]
                        },
                        "then": "1536x864"
                      },
                      {
                        "when": {
                          "$eq": [
                            {
                              "$lower": {
                                "$trim": {
                                  "$ref": "request.aspectRatio"
                                }
                              }
                            },
                            "9:16"
                          ]
                        },
                        "then": "864x1536"
                      },
                      {
                        "when": {
                          "$gt": [
                            {
                              "$len": {
                                "$split": [
                                  {
                                    "$trim": {
                                      "$ref": "request.aspectRatio"
                                    }
                                  },
                                  "x"
                                ]
                              }
                            },
                            1
                          ]
                        },
                        "then": {
                          "$trim": {
                            "$ref": "request.aspectRatio"
                          }
                        }
                      }
                    ],
                    "default": "1024x1024"
                  }
                }
              ]
            },
            "quality": {
              "$coalesce": [
                {
                  "$ref": "request.providerOptions.kbai-image.quality"
                },
                {
                  "$ref": "request.quality"
                },
                "1K"
              ]
            },
            "n": {
              "$if": {
                "condition": {
                  "$gt": [
                    {
                      "$ref": "request.imageCount"
                    },
                    0
                  ]
                },
                "then": {
                  "$ref": "request.imageCount"
                },
                "else": 1
              }
            }
          },
          "files": [
            {
              "name": "image",
              "source": {
                "$first": {
                  "$sortByOrder": {
                    "$ref": "request.images"
                  }
                }
              },
              "filename": "input.png"
            }
          ]
        },
        "response": {
          "status": "succeeded",
          "images": {
            "$coalesce": [
              {
                "$map": {
                  "from": {
                    "$ref": "response.data"
                  },
                  "as": "item",
                  "in": {
                    "url": {
                      "$coalesce": [
                        {
                          "$ref": "item.url"
                        },
                        {
                          "$ref": "item.image_url"
                        },
                        {
                          "$ref": "item.imageUrl"
                        }
                      ]
                    },
                    "dataUrl": {
                      "$if": {
                        "condition": {
                          "$ref": "item.b64_json"
                        },
                        "then": {
                          "$concat": [
                            "data:",
                            {
                              "$coalesce": [
                                {
                                  "$ref": "item.mime_type"
                                },
                                "image/png"
                              ]
                            },
                            ";base64,",
                            {
                              "$ref": "item.b64_json"
                            }
                          ]
                        },
                        "else": null
                      }
                    }
                  }
                }
              },
              {
                "$if": {
                  "condition": {
                    "$coalesce": [
                      {
                        "$ref": "response.image_url"
                      },
                      {
                        "$ref": "response.url"
                      },
                      {
                        "$ref": "response.b64_json"
                      }
                    ]
                  },
                  "then": [
                    {
                      "url": {
                        "$coalesce": [
                          {
                            "$ref": "response.image_url"
                          },
                          {
                            "$ref": "response.url"
                          }
                        ]
                      },
                      "dataUrl": {
                        "$if": {
                          "condition": {
                            "$ref": "response.b64_json"
                          },
                          "then": {
                            "$concat": [
                              "[image omitted]",
                              {
                                "$ref": "response.b64_json"
                              }
                            ]
                          },
                          "else": null
                        }
                      }
                    }
                  ],
                  "else": null
                }
              }
            ]
          },
          "errorPaths": [
            "error",
            "api_error",
            "code"
          ],
          "messagePaths": [
            "error_message",
            "error.message",
            "api_error.message",
            "message",
            "msg",
            "error"
          ]
        }
      }
    ]
  },
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>"
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
