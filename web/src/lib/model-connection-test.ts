import { runBackendGenerationTask, type GenerationTaskDependencies } from "@/services/api/generation-task";
import { defaultConfig, encodeChannelModel, type ModelCapability, type ModelChannel } from "@/stores/use-config-store";
import type { ModelProtocol } from "@/lib/model-protocols";

export async function testChannelModelConnection(channel: ModelChannel, model: string, capability: ModelCapability, protocol: ModelProtocol, dependencies?: GenerationTaskDependencies) {
    if (!channel.baseUrl.trim()) throw new Error("请先填写 Base URL");
    if (!channel.apiKey.trim()) throw new Error("请先填写 API Key");
    if (!protocol.trim()) throw new Error("请先选择请求协议插件");
    const selectedModel = encodeChannelModel(channel.id, model);
    const modelCost = channel.modelCosts?.find((item) => item.model === model);
    const testChannel: ModelChannel = {
        ...channel,
        models: channel.models.includes(model) ? channel.models : [...channel.models, model],
        modelCosts: [
            {
                model,
                displayName: modelCost?.displayName,
                capability,
                protocol,
                billingMode: modelCost?.billingMode || "fixed_request",
                unitPriceMicrocredits: modelCost?.unitPriceMicrocredits || 0,
                capabilityConfig: modelCost?.capabilityConfig,
            },
            ...(channel.modelCosts || []).filter((item) => item.model !== model),
        ],
    };
    const config = {
        ...defaultConfig,
        channelMode: "local" as const,
        baseUrl: channel.baseUrl,
        apiKey: channel.apiKey,
        apiFormat: channel.apiFormat,
        channels: [testChannel],
        model: selectedModel,
        imageModel: selectedModel,
        videoModel: selectedModel,
        textModel: selectedModel,
        audioModel: selectedModel,
        models: [selectedModel],
        imageModels: capability === "image" ? [selectedModel] : [],
        videoModels: capability === "video" ? [selectedModel] : [],
        textModels: capability === "text" ? [selectedModel] : [],
        audioModels: capability === "audio" ? [selectedModel] : [],
        count: "1",
        size: capability === "image" ? "1:1" : "16:9",
        videoSeconds: "6",
        vquality: "720",
        videoGenerateAudio: "false",
    };

    const prompts = {
        text: "Reply with OK.",
        image: "A simple gray circle on a white background.",
        audio: "Model test.",
        video: "A static gray circle on a white background.",
    };
    const result = await runBackendGenerationTask(
        {
            mode: capability,
            prompt: prompts[capability],
            config,
            streamText: false,
            metadata: { source: "channel-model-test" },
        },
        dependencies,
    );
    const hasMedia = (media?: { dataUrl: string; storageKey?: string }) => Boolean(media?.dataUrl || media?.storageKey);
    const valid = {
        text: Boolean(result.text?.trim()),
        image: Boolean(result.images?.some(hasMedia)),
        audio: hasMedia(result.audio),
        video: hasMedia(result.video),
    };
    if (result.mode !== capability || !valid[capability]) throw new Error("模型测试未返回有效结果");
    return { text: "文本响应正常", image: "图片生成正常", audio: "音频生成正常", video: "视频生成正常" }[capability];
}
