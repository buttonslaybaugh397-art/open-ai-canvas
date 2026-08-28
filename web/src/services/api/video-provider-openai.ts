import { dataUrlToFile } from "@/lib/image-utils";
import { modelCapabilityConfigFor, videoResolutionRequest } from "@/lib/model-capabilities";
import { getMediaBlob } from "@/services/file-storage";
import { imageToDataUrl } from "@/services/image-storage";
import { modelOptionName } from "@/stores/use-config-store";
import type { ReferenceImage } from "@/types/image";
import type { ReferenceAudio, ReferenceVideo } from "@/types/media";

import { normalizeVideoSeconds, normalizeVideoSize } from "./video-validation";
import type { RequestOptions, ResolvedAiConfig, ApiVideoResponse, VideoGenerationTask, VideoGenerationTaskState } from "./video-contracts";
import type { VideoProviderDeps } from "./video-provider-deps";

export async function createOpenAIVideoTask(deps: VideoProviderDeps, config: ResolvedAiConfig, model: string, prompt: string, references: ReferenceImage[], videoReferences: ReferenceVideo[] = [], audioReferences: ReferenceAudio[] = [], options?: RequestOptions): Promise<VideoGenerationTask> {
    const modelName = modelOptionName(model);
    if (config.interfaceType === "xai-video" || modelName.toLowerCase().includes("grok")) {
        const images = await Promise.all(references.slice(0, 7).map((image) => imageToDataUrl(image)));
        const seconds = normalizeVideoSeconds(config.videoSeconds);
        const payload = {
            model: modelName,
            prompt,
            duration: Number.parseInt(seconds, 10) || 6,
            seconds,
            ...(normalizeVideoSize(config.size) ? { size: normalizeVideoSize(config.size) } : {}),
            ...(images.length ? { image: images[0], images } : {}),
            ...(videoReferences.length ? { reference_videos: await referenceMediaDataUrls(videoReferences) } : {}),
            ...(audioReferences.length ? { reference_audios: await referenceMediaDataUrls(audioReferences) } : {}),
        };
        try {
            const createPath = config.interfaceType === "xai-video" ? "/videos/generations" : "/videos";
            const created = deps.response.unwrapVideoResponse(await deps.transport.post<ApiVideoResponse>(deps.transport.apiUrl(createPath), payload, options));
            const id = deps.response.videoTaskId(created);
            if (!id) throw new Error("视频接口没有返回任务 ID");
            return { id, provider: "openai", model };
        } catch (error) {
            throw new Error(deps.response.readAxiosError(error, "视频任务创建失败"));
        }
    }
    const body = new FormData();
    body.append("model", modelName);
    body.append("prompt", prompt);
    body.append("seconds", normalizeVideoSeconds(config.videoSeconds));
    if (normalizeVideoSize(config.size)) body.append("size", normalizeVideoSize(config.size)!);
    const resolution = videoResolutionRequest(modelCapabilityConfigFor(config, model).video!, config.vquality);
    if (resolution) body.append("resolution_name", resolution);
    body.append("preset", "normal");
    const imageFiles = await Promise.all(references.slice(0, 7).map(async (image) => dataUrlToFile({ ...image, dataUrl: await imageToDataUrl(image) })));
    const mediaFiles = await Promise.all([
        ...videoReferences.slice(0, 3).map(mediaToFile),
        ...audioReferences.slice(0, 3).map(mediaToFile),
    ]);
    [...imageFiles, ...mediaFiles].forEach((file) => body.append("input_reference[]", file));
    try {
        const created = deps.response.unwrapVideoResponse(await deps.transport.postForm<ApiVideoResponse>(deps.transport.apiUrl("/videos"), body, options));
        if (!created.id) throw new Error("视频接口没有返回任务 ID");
        return { id: created.id, provider: "openai", model };
    } catch (error) {
        throw new Error(deps.response.readAxiosError(error, "视频任务创建失败"));
    }
}

async function referenceMediaDataUrls(items: Array<ReferenceVideo | ReferenceAudio>) {
    return Promise.all(items.slice(0, 3).map(async (item) => {
        const blob = item.storageKey ? await getMediaBlob(item.storageKey) : null;
        return blob ? blobToDataUrl(blob) : item.url;
    }));
}

async function mediaToFile(media: ReferenceVideo | ReferenceAudio) {
    const blob = media.storageKey ? await getMediaBlob(media.storageKey) : null;
    const resolved = blob || (media.url ? await (await fetch(media.url)).blob() : null);
    if (!resolved) throw new Error(`${media.name || "参考媒体"} 无法读取，请重新上传后再试`);
    return new File([resolved], media.name || `reference.${media.type.split("/")[1] || "bin"}`, { type: media.type || resolved.type || "application/octet-stream" });
}

function blobToDataUrl(blob: Blob) {
    return new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result || ""));
        reader.onerror = () => reject(new Error("读取参考媒体失败"));
        reader.readAsDataURL(blob);
    });
}

export async function pollOpenAIVideoTask(deps: VideoProviderDeps, task: VideoGenerationTask, options?: RequestOptions): Promise<VideoGenerationTaskState> {
    try {
        const video = deps.response.unwrapVideoResponse(await deps.transport.get<ApiVideoResponse>(deps.transport.apiUrl(`/videos/${task.id}`), options));
        if (video.status === "completed" || video.status === "succeeded" || video.status === "success" || video.status === "done") {
            const resultUrl = video.video?.url || video.video_url || video.result_url;
            if (resultUrl) return { status: "completed", result: await deps.response.videoResultFromUrl(resultUrl, options) };
            const content = await deps.transport.getBlob(deps.transport.apiUrl(`/videos/${task.id}/content`), options);
            await deps.response.assertVideoBlob(content);
            return { status: "completed", result: { blob: content } };
        }
        if (video.status === "failed" || video.status === "cancelled") return { status: "failed", error: video.error?.message || "视频生成失败" };
        return { status: "pending" };
    } catch (error) {
        throw new Error(deps.response.readAxiosError(error, "视频任务查询失败"));
    }
}
