import { modelCapabilityConfigFor } from "@/lib/model-capabilities";
import { importResourceFromUrl } from "@/services/api/resources";
import { uploadMediaFile, type UploadedFile } from "@/services/file-storage";
import { resolveModelRequestConfig, type AiConfig } from "@/stores/use-config-store";
import type { ReferenceImage } from "@/types/image";
import type { ReferenceAudio, ReferenceVideo } from "@/types/media";

import { assertVideoCapability, assertVideoConfig } from "./video-validation";
import type { RequestOptions, VideoGenerationResult, VideoGenerationTask, VideoGenerationTaskState } from "./video-contracts";
import { videoResponseTools } from "./video-response";
import type { VideoProviderDeps } from "./video-provider-deps";
import { createAgnesVideoTask, isAgnesConfig, pollAgnesVideoTask } from "./video-provider-agnes";
import { createGeminiVeoTask, pollGeminiVeoTask } from "./video-provider-gemini";
import { createMiniMaxVideoTask, pollMiniMaxVideoTask } from "./video-provider-minimax";
import { createVideoGenerationsTask, pollVideoGenerationsTask } from "./video-provider-newapi";
import { createNovitaVideoTask, pollNovitaVideoTask } from "./video-provider-novita";
import { createOpenAIVideoTask, pollOpenAIVideoTask } from "./video-provider-openai";
import { createSeedanceTask, isSeedanceConfig, pollSeedanceTask } from "./video-provider-seedance";
import { createVideoTransport } from "./video-transport";

export type { VideoGenerationResult, VideoGenerationTask, VideoGenerationTaskState } from "./video-contracts";

export async function requestVideoGeneration(config: AiConfig, prompt: string, references: ReferenceImage[] = [], videoReferences: ReferenceVideo[] = [], audioReferences: ReferenceAudio[] = [], options?: RequestOptions): Promise<VideoGenerationResult> {
    const task = await createVideoGenerationTask(config, prompt, references, videoReferences, audioReferences, options);
    const delayMs = task.provider === "agnes" ? 1500 : task.provider === "openai" ? 2500 : 5000;
    for (let attempt = 0; attempt < 120; attempt += 1) {
        if (options?.signal?.aborted) throw new DOMException("Aborted", "AbortError");
        const state = await pollVideoGenerationTask(config, task, options);
        if (state.status === "completed") return state.result;
        if (state.status === "failed") throw new Error(state.error);
        if (attempt === 119) throw new Error(`${task.provider === "seedance" ? "Seedance " : ""}视频生成超时，请稍后重试`);
        await videoResponseTools.delay(delayMs, options?.signal);
    }
    throw new Error("视频生成超时，请稍后重试");
}

export async function createVideoGenerationTask(config: AiConfig, prompt: string, references: ReferenceImage[] = [], videoReferences: ReferenceVideo[] = [], audioReferences: ReferenceAudio[] = [], options?: RequestOptions): Promise<VideoGenerationTask> {
    const selectedModel = (config.model || config.videoModel).trim();
    const requestConfig = resolveModelRequestConfig(config, selectedModel);
    assertVideoConfig(requestConfig, requestConfig.model);
    assertVideoCapability(modelCapabilityConfigFor(config, selectedModel).video!, references, videoReferences, audioReferences, config.videoSeconds);
    const deps: VideoProviderDeps = { transport: createVideoTransport(requestConfig), response: videoResponseTools };
    if (requestConfig.interfaceType === "newapi-channel-2") return createVideoGenerationsTask(deps, requestConfig, selectedModel, prompt, references, videoReferences, audioReferences, options);
    if (requestConfig.interfaceType === "gemini-veo") return createGeminiVeoTask(deps, requestConfig, selectedModel, prompt, references, videoReferences, audioReferences, options);
    if (requestConfig.interfaceType === "novita-video") return createNovitaVideoTask(deps, requestConfig, selectedModel, prompt, references, videoReferences, audioReferences, options);
    if (requestConfig.interfaceType === "minimax-video") return createMiniMaxVideoTask(deps, requestConfig, selectedModel, prompt, references, videoReferences, audioReferences, options);
    if (isAgnesConfig(requestConfig)) return createAgnesVideoTask(deps, requestConfig, selectedModel, prompt, references, videoReferences, audioReferences, options);
    if (isSeedanceConfig(requestConfig)) return createSeedanceTask(deps, requestConfig, selectedModel, prompt, references, videoReferences, audioReferences, options);
    return createOpenAIVideoTask(deps, requestConfig, selectedModel, prompt, references, options);
}

export async function pollVideoGenerationTask(config: AiConfig, task: VideoGenerationTask, options?: RequestOptions): Promise<VideoGenerationTaskState> {
    const requestConfig = resolveModelRequestConfig(config, task.model);
    assertVideoConfig(requestConfig, requestConfig.model);
    const deps: VideoProviderDeps = { transport: createVideoTransport(requestConfig), response: videoResponseTools };
    if (task.provider === "video-generations") return pollVideoGenerationsTask(deps, task, options);
    if (task.provider === "gemini-veo") return pollGeminiVeoTask(deps, requestConfig, task, options);
    if (task.provider === "novita") return pollNovitaVideoTask(deps, requestConfig, task, options);
    if (task.provider === "minimax") return pollMiniMaxVideoTask(deps, requestConfig, task, options);
    if (task.provider === "agnes") return pollAgnesVideoTask(deps, requestConfig, task, options);
    if (task.provider === "seedance") return pollSeedanceTask(deps, requestConfig, task, options);
    return pollOpenAIVideoTask(deps, task, options);
}

export async function storeGeneratedVideo(result: VideoGenerationResult): Promise<UploadedFile> {
    if (!result.blob && result.dataUrl && /^https?:\/\//i.test(result.dataUrl)) {
        return importGeneratedVideo(result.dataUrl, result.url || result.dataUrl, result.mimeType);
    }
    const backupBlob =
        result.blob ||
        (result.dataUrl
            ? await fetch(result.dataUrl).then((response) => {
                  if (!response.ok) throw new Error(`视频备份读取失败（${response.status}）`);
                  return response.blob();
              })
            : undefined);
    if (backupBlob) {
        // Generated videos must have a server resource record. The backend can
        // still keep a local disaster-recovery copy when OSS is unavailable.
        const uploaded = await uploadMediaFile(backupBlob, "video", { allowLocalFallback: false });
        return { ...uploaded, url: result.url || uploaded.url };
    }
    if (result.url) {
        let lastError: unknown = null;
        for (let attempt = 0; attempt < 3; attempt += 1) {
            try {
                return await importGeneratedVideo(result.url, result.url, result.mimeType);
            } catch (error) {
                lastError = error;
            }
            if (attempt < 2) await new Promise((resolve) => window.setTimeout(resolve, 300 * (attempt + 1)));
        }
        throw lastError instanceof Error ? lastError : new Error("视频同步到服务器资源存储失败");
    }
    throw new Error("视频接口没有返回可播放的视频");
}

async function importGeneratedVideo(sourceUrl: string, previewUrl: string, mimeType?: string): Promise<UploadedFile> {
    const resource = await importResourceFromUrl(sourceUrl, "video");
    return {
        url: previewUrl,
        storageKey: `resource:${resource.id}`,
        bytes: resource.size || 0,
        mimeType: resource.mimeType || mimeType || "video/mp4",
        width: resource.width,
        height: resource.height,
        durationMs: resource.durationMs,
    };
}

export type { VideoProviderDeps } from "./video-provider-deps";
