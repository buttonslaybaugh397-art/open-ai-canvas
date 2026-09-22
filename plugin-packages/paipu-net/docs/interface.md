# 牌谱 API 接口字段

此包保留本地已有的 New API 映射，仅适配影策 v2 声明式插件格式。请求字段按模型能力组装，避免把某个模型的专属参数发送给其他模型。以下说明描述协议行为，不代表每个模型都支持所有字段。

`providerOptions.paipu-net-image` 和 `providerOptions.paipu-net-video` 可按模型覆盖请求字段。覆盖值会先经过模型能力过滤；空字符串、零值和空媒体数组不会被发送。

## 配置

`apiKey` 为牌谱 API Key，由宿主写入 Bearer Authorization。基地址为 `https://api.paipu.net`，密钥不得出现在 URL 或日志中。

## 参数

| 模型 | 请求参数 |
| --- | --- |
| Image 2.5 Flare / Sunburst | `model`、`prompt`、`aspect_ratio`、`resolution`、`output_format`、`response_format`、`images`。 |
| Banana Flash / Pro | `model`、`prompt`、`aspect_ratio`、`resolution`、`images`。 |
| TinySnow Image 2 | `lec-tinysnow-image-2`，支持 `n`、`quality`、`aspect_ratio`、`resolution`、`images`，固定 `response_format: b64_json`。 |
| Image 2 | 支持 `output_format`，不传 `n`、`quality`、`response_format`。 |
| Seedream 5 Pro | `model`、`prompt`、`resolution`、`aspect_ratio`、`images`。 |
| H3 Video 2K | `model`、`prompt`、`aspect_ratio`、`images`，不传时长、清晰度、参考视频或音频。 |
| Seedance / Wan / Grok 等视频 | 按公开模型 ID 选择字段；具体字段见下方生成合同。 |
| 天悦人脸处理 | 必须恰好一张参考图；发送 `mode`（默认 `黑白素描1`）、`aspect_ratio`、`images`，不发送 `prompt`。 |

未知视频模型只发送 `model` 和 `prompt`；增加模型专属字段需要更新协议映射。空 `images`、`videos`、`audios` 数组会省略。

文本保留 `temperature`、`top_p`、`max_completion_tokens` 的既有额外参数映射。Agent 请求映射宿主生成的 messages、tools 和 tool_choice。

连接测试会真实创建后端任务，并使用当前模型的默认参数；测试失败会反映后端实际错误。

## 响应

- 文本创建 `POST /v1/chat/completions`，提取 `choices.0.message.content` 或 `choices.0.text`；工具调用按 Chat Completions 字段解析。
- 图片创建 `POST /v1/images/generations`，支持 `data` 数组或顶层的 URL / `b64_json` 结果。
- 视频创建 `POST /v1/videos`，提取 `id` 或 `task_id`；查询 `GET /v1/videos/{taskId}`，读取 `status`。
- 视频结果支持 `url`、`video_url`、`result_url`、`data.url`、`data.0.url`、`data.video_url`、`data.0.video_url`、`metadata.url`；错误读取 `message`、`error.message`、`error.code`。
- `queued` / `pending` 为排队；`REFERENCE_MATERIALIZING`、`SUBMITTING`、`UPSTREAM_PROCESSING`、`RESULT_STORAGE_WAIT` 等为处理中。未知非空状态、明确失败及完成却无结果会报错。
- 临时结果由宿主持久化；连接测试复用后端任务链路，并携带模型默认参数。

## 打包

运行 `bun plugin-packages/paipu-net/build.ts` 会将 README 与本文写入安装包文档，并从当前 manifest 自动生成完整接口合同。不要手工修改下方生成区域。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

```json
{
  "apiVersion": "yingce.plugin/v2",
  "id": "paipu-net",
  "name": "牌谱 API",
  "version": "1.0.3",
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
            "name": "aspect_ratio",
            "type": "string",
            "mapping": "aspectRatio",
            "description": "模型支持时传递画面比例。"
          },
          {
            "name": "resolution",
            "type": "string",
            "mapping": "resolution",
            "description": "模型支持时传递输出清晰度。"
          },
          {
            "name": "quality",
            "type": "string",
            "mapping": "quality",
            "description": "仅 TinySnow 支持；auto 不发送。"
          },
          {
            "name": "n",
            "type": "integer",
            "mapping": "imageCount",
            "description": "仅 TinySnow 支持输出数量。"
          },
          {
            "name": "output_format",
            "type": "string",
            "mapping": "outputFormat",
            "description": "仅 Image 2 / Image 2.5 支持。"
          },
          {
            "name": "response_format",
            "type": "string",
            "mapping": "responseFormat",
            "description": "仅 Image 2.5 Flare/Sunburst 支持。"
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
            "$switch": {
              "cases": [
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      [
                        "lec-ac-image-2-5-flare",
                        "lec-ac-image-2-5-sunburst"
                      ]
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "16:9"
                        ]
                      }
                    },
                    "resolution": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.resolution"
                          },
                          {
                            "$ref": "request.resolution"
                          },
                          "1K"
                        ]
                      }
                    },
                    "output_format": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.output_format"
                          },
                          "jpeg"
                        ]
                      }
                    },
                    "response_format": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.response_format"
                          },
                          "url"
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
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
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      [
                        "lec-ac-banana-flash",
                        "lec-ac-banana-pro"
                      ]
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "16:9"
                        ]
                      }
                    },
                    "resolution": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.resolution"
                          },
                          {
                            "$ref": "request.resolution"
                          },
                          "1K"
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
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
                {
                  "when": {
                    "$eq": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      "lec-tinysnow-image-2"
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "n": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.n"
                          },
                          {
                            "$ref": "request.imageCount"
                          },
                          1
                        ]
                      }
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "1:1"
                        ]
                      }
                    },
                    "resolution": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.resolution"
                          },
                          {
                            "$ref": "request.resolution"
                          },
                          "1K"
                        ]
                      }
                    },
                    "quality": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.quality"
                          },
                          {
                            "$ref": "request.quality"
                          }
                        ]
                      }
                    },
                    "response_format": "b64_json"
                  }
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      "lec-ac-image-2"
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "16:9"
                        ]
                      }
                    },
                    "resolution": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.resolution"
                          },
                          {
                            "$ref": "request.resolution"
                          },
                          "1K"
                        ]
                      }
                    },
                    "output_format": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.output_format"
                          },
                          "jpeg"
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
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
                {
                  "when": {
                    "$eq": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      "lec-ty-seedream-5-pro"
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "resolution": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.resolution"
                          },
                          {
                            "$ref": "request.resolution"
                          },
                          "1K"
                        ]
                      }
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-image.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "1:1"
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    }
                  }
                }
              ],
              "default": {
                "model": {
                  "$ref": "request.model"
                },
                "prompt": {
                  "$ref": "request.prompt"
                },
                "images": {
                  "$omitEmpty": {
                    "$map": {
                      "from": {
                        "$ref": "request.images"
                      },
                      "as": "media",
                      "in": {
                        "$ref": "media.value"
                      }
                    }
                  }
                }
              }
            }
          }
        },
        "response": {
          "images": {
            "$coalesce": [
              {
                "$ref": "response.data"
              },
              {
                "$ref": "response"
              }
            ]
          },
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
            "mapping": "duration",
            "description": "模型支持时传递视频时长。"
          },
          {
            "name": "aspect_ratio",
            "type": "string",
            "mapping": "aspectRatio",
            "description": "模型支持时传递画面比例。"
          },
          {
            "name": "resolution",
            "type": "string",
            "mapping": "resolution",
            "description": "模型支持时传递分辨率。"
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
          },
          {
            "name": "mode",
            "type": "string",
            "mapping": "mode",
            "description": "仅天悦人脸处理模型支持。"
          },
          {
            "name": "process_face",
            "type": "boolean",
            "mapping": "processFace",
            "description": "仅 MD Seedance 模型支持。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/v1/videos",
          "contentType": "application/json",
          "body": {
            "$switch": {
              "cases": [
                {
                  "when": {
                    "$eq": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      "lec-ac-seedance-2-0-fast-2-720p"
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "16:9"
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "videos": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.videos"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "audios": {
                      "$omitEmpty": {
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
                  }
                },
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      [
                        "lec-ac-seedance-2-5-10-image",
                        "lec-h3video-2k",
                        "lec-seedance-2-5-30s",
                        "lec-bk-video-30s",
                        "lec-seed-2-0-900",
                        "lec-seed-2-5-900"
                      ]
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "16:9"
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
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
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      [
                        "lec-minimax-h3-768p",
                        "lec-seedance-2-0-mini-c4-480p"
                      ]
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "duration": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.duration"
                          },
                          {
                            "$ref": "request.duration"
                          },
                          5
                        ]
                      }
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "16:9"
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "audios": {
                      "$omitEmpty": {
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
                  }
                },
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      [
                        "lec-ac-seedance-2-5-vid",
                        "lec-haya-seedance-2-5-480p",
                        "lec-vp-seedance-2-0-933-s1",
                        "lec-vp-seedance-2-0-933-s2",
                        "lec-minimax-h3",
                        "lec-seedance-2-0-full-933-720p",
                        "lec-seedance-2-0-fast-933-720p",
                        "lec-ty-seedance-2-0-full-933-me-720p",
                        "lec-ty-seedance-2-0-mini-933-j-480p",
                        "lec-ty-seedance-2-0-mini-933-j-720p",
                        "lec-seedance-2-5-301010-wd-480p",
                        "lec-vg-seedance-2-5-wd",
                        "lec-mj-seedance-2-0-full-933-jd",
                        "lec-mj-seedance-2-0-full-933-md",
                        "lec-mj-seedance-2-5-md-480p",
                        "lec-sz-sd2-full-480p"
                      ]
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "duration": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.duration"
                          },
                          {
                            "$ref": "request.duration"
                          },
                          5
                        ]
                      }
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "16:9"
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "videos": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.videos"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "audios": {
                      "$omitEmpty": {
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
                  }
                },
                {
                  "when": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      [
                        "lec-haya-seedance-2-5-sm-pro",
                        "lec-vp-seedance-2-5-m2",
                        "lec-vp-wan-3-0",
                        "lec-vp-wan-3-0-prime",
                        "lec-seedance-2-0-933-stable",
                        "lec-vg-seedance-2-5-kk",
                        "lec-mj-seedance-2-5-md",
                        "lec-gt-seedance-2-0-mini",
                        "lec-gt-seedance-2-0-full",
                        "lec-gt-seedance-2-5-720p",
                        "lec-wan3-720p"
                      ]
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "duration": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.duration"
                          },
                          {
                            "$ref": "request.duration"
                          },
                          5
                        ]
                      }
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "16:9"
                        ]
                      }
                    },
                    "resolution": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.resolution"
                          },
                          {
                            "$ref": "request.resolution"
                          }
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "videos": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.videos"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "audios": {
                      "$omitEmpty": {
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
                  }
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      "lec-yu25-grok-video-1-5-preview"
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "duration": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.duration"
                          },
                          {
                            "$ref": "request.duration"
                          },
                          5
                        ]
                      }
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "16:9"
                        ]
                      }
                    },
                    "resolution": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.resolution"
                          },
                          {
                            "$ref": "request.resolution"
                          }
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "image": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.image"
                          }
                        ]
                      }
                    }
                  }
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      "lec-md-seedance-2-0-900-720p"
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "prompt": {
                      "$ref": "request.prompt"
                    },
                    "duration": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.duration"
                          },
                          {
                            "$ref": "request.duration"
                          },
                          5
                        ]
                      }
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "16:9"
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "process_face": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.process_face"
                          }
                        ]
                      }
                    }
                  }
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      "lec-ty-face-processing-1-0"
                    ]
                  },
                  "then": {
                    "model": {
                      "$ref": "request.model"
                    },
                    "aspect_ratio": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.aspect_ratio"
                          },
                          {
                            "$ref": "request.aspectRatio"
                          },
                          "16:9"
                        ]
                      }
                    },
                    "images": {
                      "$omitEmpty": {
                        "$map": {
                          "from": {
                            "$ref": "request.images"
                          },
                          "as": "media",
                          "in": {
                            "$ref": "media.value"
                          }
                        }
                      }
                    },
                    "mode": {
                      "$omitEmpty": {
                        "$coalesce": [
                          {
                            "$ref": "request.providerOptions.paipu-net-video.mode"
                          },
                          "黑白素描1"
                        ]
                      }
                    }
                  }
                }
              ],
              "default": {
                "model": {
                  "$ref": "request.model"
                },
                "prompt": {
                  "$ref": "request.prompt"
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
            "task_id",
            "request_id"
          ],
          "statusPaths": [
            "status",
            "state"
          ],
          "errorPaths": [
            "error",
            "error.code",
            "error.type"
          ],
          "messagePaths": [
            "message",
            "error.message",
            "error.detail",
            "error.code"
          ],
          "resultPaths": [
            "url",
            "video_url",
            "result_url",
            "data.url",
            "data.0.url",
            "data.video_url",
            "data.0.video_url",
            "metadata.url"
          ],
          "resultKind": "video",
          "resultEphemeral": true
        },
        "validations": [
          {
            "assert": {
              "$or": [
                {
                  "$not": {
                    "$in": [
                      {
                        "$lower": {
                          "$trim": {
                            "$ref": "request.model"
                          }
                        }
                      },
                      [
                        "lec-ac-seedance-2-5-10-image",
                        "lec-minimax-h3",
                        "lec-ty-seedance-2-0-full-933-me-720p",
                        "lec-ty-seedance-2-0-mini-933-j-480p",
                        "lec-ty-seedance-2-0-mini-933-j-720p",
                        "lec-ty-seedance-2-5-301010-wd-480p",
                        "lec-sz-sd2-full-480p",
                        "lec-ty-face-processing-1-0"
                      ]
                    ]
                  }
                },
                {
                  "$gt": [
                    {
                      "$len": {
                        "$ref": "request.images"
                      }
                    },
                    0
                  ]
                }
              ]
            },
            "message": "该牌谱视频模型必须至少提供 1 张参考图片。"
          },
          {
            "assert": {
              "$or": [
                {
                  "$ne": [
                    {
                      "$lower": {
                        "$trim": {
                          "$ref": "request.model"
                        }
                      }
                    },
                    "lec-ty-face-processing-1-0"
                  ]
                },
                {
                  "$eq": [
                    {
                      "$len": {
                        "$ref": "request.images"
                      }
                    },
                    1
                  ]
                }
              ]
            },
            "message": "lec-ty-face-processing-1-0 必须恰好提供 1 张参考图片。"
          }
        ]
      }
    ]
  }
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
