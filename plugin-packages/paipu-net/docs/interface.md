# 牌谱 API 接口字段

此包保留本地已有的 New API 映射，仅适配影策 v2 声明式插件格式。不同模型的可用参数仍以服务商返回的能力为准；本次没有发送真实计费请求。

## 配置

`apiKey` 为牌谱 API Key，由宿主写入 Bearer Authorization。基地址为 `https://api.paipu.net`，密钥不得出现在 URL 或日志中。

## 参数

| 字段 | 上游映射 | 说明 |
| --- | --- | --- |
| `prompt` | 文本 `messages`，图片/视频 `prompt` | 文本消息由宿主合成。 |
| `size` | 图片 `size` | 取宿主 `aspectRatio`，不擅自改写模型尺寸。 |
| `quality` | 图片 `quality` | 空、auto、default 不发送。 |
| `n` | 图片 `n` | 取宿主 `imageCount`，零值省略。 |
| `duration` | 视频 `duration` | 整数秒数，零值省略。 |
| `aspect_ratio` | 视频 `aspect_ratio` | 取宿主 `aspectRatio`，空值省略。 |
| `resolution` | 视频 `resolution` | 空、auto、default 不发送。 |
| `images` | 图片/视频 `images` | 媒体引用地址数组。 |
| `videos` | 视频 `videos` | 参考视频地址数组。 |
| `audios` | 视频 `audios` | 参考音频地址数组。 |

文本保留 `temperature`、`top_p`、`max_completion_tokens` 的既有额外参数映射。Agent 请求映射宿主生成的 messages、tools 和 tool_choice。

## 响应

- 文本创建 `POST /v1/chat/completions`，提取 `choices.0.message.content` 或 `choices.0.text`；工具调用按 Chat Completions 字段解析。
- 图片创建 `POST /v1/images/generations`，提取 `data.0.url` 或 `url`。这里保留原有首张结果映射，不代表已验证所有批量图片输出。
- 视频创建 `POST /v1/videos`，提取 `id` 或 `task_id`；查询 `GET /v1/videos/{taskId}`，读取 `status`。
- 视频结果地址依次读取 `url`、`result_url`、`metadata.url`；错误读取 `message`、`error.message`、`error.code`。
- 临时结果由宿主持久化。状态归一化、缺失结果和重试处理使用影策统一协议运行时。

## 打包

运行 `bun plugin-packages/paipu-net/build.ts` 会将 README 与本文写入安装包文档，并从当前 manifest 自动生成完整接口合同。不要手工修改下方生成区域。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "paipu-net",
  "name": "牌谱 API",
  "version": "1.0.1",
  "author": "影策社区",
  "description": "接入 api.paipu.net 的 New API 兼容文本、图片与视频模型接口。",
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>",
  "permissions": [
    "generation.run",
    "media.read"
  ],
  "configuration": {
    "fields": [
      {
        "name": "apiKey",
        "type": "secret",
        "label": "牌谱 API Key",
        "required": true,
        "description": "填写 api.paipu.net 控制台生成的 API Key。"
      }
    ]
  },
  "contributes": {
    "providers": [
      {
        "id": "paipu-net-text",
        "label": "牌谱 API 文本",
        "capabilities": [
          "text"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation",
          "agent"
        ],
        "baseUrl": "https://api.paipu.net",
        "auth": {
          "type": "bearer",
          "field": "apiKey",
          "header": "Authorization"
        },
        "parameters": [
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "文本提示词，由宿主合成 messages。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/chat/completions",
          "contentType": "application/json",
          "body": {
            "model": {
              "$ref": "request.model"
            },
            "messages": {
              "$ref": "request.messages"
            },
            "temperature": {
              "$if": {
                "condition": {
                  "$ref": "request.extra.temperature"
                },
                "then": {
                  "$ref": "request.extra.temperature"
                }
              }
            },
            "top_p": {
              "$if": {
                "condition": {
                  "$ref": "request.extra.top_p"
                },
                "then": {
                  "$ref": "request.extra.top_p"
                }
              }
            },
            "max_completion_tokens": {
              "$if": {
                "condition": {
                  "$ref": "request.extra.max_completion_tokens"
                },
                "then": {
                  "$ref": "request.extra.max_completion_tokens"
                }
              }
            }
          }
        },
        "agent": {
          "method": "POST",
          "path": "/v1/chat/completions",
          "contentType": "application/json",
          "body": {
            "model": {
              "$ref": "request.model"
            },
            "messages": {
              "$ref": "request.extra.agent.chatCompletion.messages"
            },
            "tools": {
              "$ref": "request.extra.agent.chatCompletion.tools"
            },
            "tool_choice": {
              "$ref": "request.extra.agent.chatCompletion.tool_choice"
            }
          }
        },
        "agentResponse": {
          "textPaths": [
            "choices.0.message.content"
          ],
          "reasoningPaths": [
            "choices.0.message.reasoning_content"
          ],
          "toolCallsPath": "choices.0.message.tool_calls",
          "toolCallIdPaths": [
            "id"
          ],
          "toolCallNamePaths": [
            "function.name"
          ],
          "toolCallArgumentsPaths": [
            "function.arguments"
          ]
        },
        "response": {
          "textPaths": [
            "choices.0.message.content",
            "choices.0.text"
          ],
          "reasoningPaths": [
            "choices.0.message.reasoning_content"
          ]
        }
      },
      {
        "id": "paipu-net-image",
        "label": "牌谱 API 图片",
        "capabilities": [
          "image"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation"
        ],
        "baseUrl": "https://api.paipu.net",
        "auth": {
          "type": "bearer",
          "field": "apiKey",
          "header": "Authorization"
        },
        "parameters": [
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "图片提示词。"
          },
          {
            "name": "size",
            "type": "string",
            "mapping": "aspectRatio",
            "description": "原样传递宿主图片尺寸。"
          },
          {
            "name": "quality",
            "type": "string",
            "mapping": "quality",
            "description": "画质，自动档不传。"
          },
          {
            "name": "n",
            "type": "integer",
            "mapping": "imageCount",
            "description": "图片数量。"
          },
          {
            "name": "images",
            "type": "media[]",
            "mapping": "images",
            "description": "参考图片地址列表。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/images/generations",
          "contentType": "application/json",
          "body": {
            "model": {
              "$ref": "request.model"
            },
            "prompt": {
              "$ref": "request.prompt"
            },
            "images": {
              "$map": {
                "from": {
                  "$ref": "request.images"
                },
                "as": "media",
                "in": {
                  "$ref": "media.value"
                }
              }
            },
            "size": {
              "$omitEmpty": {
                "$ref": "request.aspectRatio"
              }
            },
            "quality": {
              "$if": {
                "condition": {
                  "$in": [
                    {
                      "$lower": {
                        "$trim": {
                          "$ref": "request.quality"
                        }
                      }
                    },
                    [
                      "",
                      "auto",
                      "default"
                    ]
                  ]
                },
                "else": {
                  "$ref": "request.quality"
                }
              }
            },
            "n": {
              "$if": {
                "condition": {
                  "$ref": "request.imageCount"
                },
                "then": {
                  "$ref": "request.imageCount"
                }
              }
            },
            "response_format": "url"
          }
        },
        "response": {
          "resultPaths": [
            "data.0.url",
            "url"
          ],
          "resultKind": "image",
          "resultEphemeral": true
        }
      },
      {
        "id": "paipu-net-video",
        "label": "牌谱 API 视频",
        "capabilities": [
          "video"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation"
        ],
        "baseUrl": "https://api.paipu.net",
        "auth": {
          "type": "bearer",
          "field": "apiKey",
          "header": "Authorization"
        },
        "parameters": [
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "prompt",
            "description": "视频提示词。"
          },
          {
            "name": "duration",
            "type": "integer",
            "required": true,
            "mapping": "duration",
            "description": "生成时长，单位为秒。"
          },
          {
            "name": "aspect_ratio",
            "type": "string",
            "mapping": "aspectRatio",
            "description": "画面比例。"
          },
          {
            "name": "resolution",
            "type": "string",
            "mapping": "resolution",
            "description": "分辨率，自动档不传。"
          },
          {
            "name": "images",
            "type": "media[]",
            "mapping": "images",
            "description": "参考图片地址列表。"
          },
          {
            "name": "videos",
            "type": "media[]",
            "mapping": "videos",
            "description": "参考视频地址列表。"
          },
          {
            "name": "audios",
            "type": "media[]",
            "mapping": "audios",
            "description": "参考音频地址列表。"
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
            "duration": {
              "$if": {
                "condition": {
                  "$ref": "request.duration"
                },
                "then": {
                  "$ref": "request.duration"
                }
              }
            },
            "aspect_ratio": {
              "$omitEmpty": {
                "$ref": "request.aspectRatio"
              }
            },
            "resolution": {
              "$if": {
                "condition": {
                  "$in": [
                    {
                      "$lower": {
                        "$trim": {
                          "$ref": "request.resolution"
                        }
                      }
                    },
                    [
                      "",
                      "auto",
                      "default"
                    ]
                  ]
                },
                "else": {
                  "$ref": "request.resolution"
                }
              }
            },
            "images": {
              "$map": {
                "from": {
                  "$ref": "request.images"
                },
                "as": "media",
                "in": {
                  "$ref": "media.value"
                }
              }
            },
            "videos": {
              "$map": {
                "from": {
                  "$ref": "request.videos"
                },
                "as": "media",
                "in": {
                  "$ref": "media.value"
                }
              }
            },
            "audios": {
              "$map": {
                "from": {
                  "$ref": "request.audios"
                },
                "as": "media",
                "in": {
                  "$ref": "media.value"
                }
              }
            }
          }
        },
        "poll": {
          "method": "GET",
          "path": "/v1/videos/{{taskId}}"
        },
        "response": {
          "taskIdPaths": [
            "id",
            "task_id"
          ],
          "statusPaths": [
            "status"
          ],
          "errorPaths": [
            "error.code"
          ],
          "messagePaths": [
            "message",
            "error.message",
            "error.code"
          ],
          "resultPaths": [
            "url",
            "result_url",
            "metadata.url"
          ],
          "resultKind": "video",
          "resultEphemeral": true
        }
      }
    ]
  }
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
