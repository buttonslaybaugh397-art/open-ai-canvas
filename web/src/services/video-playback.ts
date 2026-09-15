import { getActiveUserScope } from "@/lib/user-scope";
import { ApiError } from "@/services/api/request";
import { playbackVariantUrl, refreshResource, requestResourcePlayback, resourceIdFromFileUrl, resourceIdFromStorageKey, type RemoteResource } from "@/services/api/resources";

export type VideoPlaybackState = {
    src: string;
    phase: "original" | "preparing" | "compatible" | "failed";
    message: string;
};

type PlaybackResource = Pick<RemoteResource, "id" | "status" | "playbackStatus">;
type PlaybackDependencies = {
    request: (id: string, signal: AbortSignal) => Promise<PlaybackResource>;
    refresh: (id: string, signal: AbortSignal) => Promise<PlaybackResource>;
    scope: () => string;
    variantUrl: (id: string) => string;
    sleep: (signal: AbortSignal) => Promise<void>;
};

const preparingMessage = "正在生成兼容预览…";
const failedMessage = "兼容预览生成失败，可下载原片后用本地播放器观看。";
const timeoutMessage = "兼容预览等待超时，请稍后重新打开预览。";
const aborted = () => new DOMException("视频预览已取消", "AbortError");

export function isVideoDecodeError(code?: number) {
    return code === 3 || code === 4;
}

export function videoPlaybackResourceId(src: string, storageKey?: string) {
    return resourceIdFromStorageKey(storageKey) || resourceIdFromFileUrl(src);
}

// Share active polling, not only the POST: several views of one resource must
// not multiply requests. Cancelling one view leaves other subscribers intact.
export function createVideoPlaybackResolver(deps: PlaybackDependencies) {
    const jobs = new Map<string, { promise: Promise<string>; controller: AbortController; subscribers: number }>();
    const ready = new Map<string, string>();
    const keyFor = (id: string) => `${deps.scope()}:${id}`;

    async function prepare(id: string, scope: string, signal: AbortSignal) {
        const check = () => {
            if (signal.aborted || deps.scope() !== scope) throw signal.reason || aborted();
        };
        check();
        let resource = await deps.request(id, signal);
        let failures = 0;
        for (let poll = 0; poll <= 120; poll++) {
            check();
            if (resource.id !== id || resource.status !== "ready") throw new Error("原视频资源不可用，请重新同步素材。");
            if (resource.playbackStatus === "ready") return deps.variantUrl(id);
            if (resource.playbackStatus === "failed") throw new Error(failedMessage);
            if (resource.playbackStatus !== "processing") throw new Error("服务端未启动兼容预览，请检查转码服务。");
            if (poll === 120) break;
            await deps.sleep(signal);
            check();
            try {
                resource = await deps.refresh(id, signal);
                failures = 0;
            } catch (error) {
                check();
                if (error instanceof ApiError && error.status && error.status < 500) throw error;
                if (++failures >= 4) throw new Error("兼容预览服务暂不可达，请稍后重试。");
            }
        }
        throw new Error(timeoutMessage);
    }

    function resolve(id: string, signal: AbortSignal) {
        if (signal.aborted) return Promise.reject(signal.reason || aborted());
        const scope = deps.scope();
        const key = keyFor(id);
        const cached = ready.get(key);
        if (cached) return Promise.resolve(cached);
        let job = jobs.get(key);
        if (!job) {
            const controller = new AbortController();
            const deadline = setTimeout(() => controller.abort(new Error(timeoutMessage)), 5 * 60_000);
            const promise = prepare(id, scope, controller.signal)
                .then((url) => {
                    if (controller.signal.aborted || deps.scope() !== scope) throw controller.signal.reason || aborted();
                    // Bound the session cache; no media bytes or cross-account URLs are persisted.
                    if (ready.size >= 256) ready.delete(ready.keys().next().value!);
                    ready.set(key, url);
                    return url;
                })
                .catch((error) => {
                    throw controller.signal.aborted ? controller.signal.reason || aborted() : error;
                })
                .finally(() => {
                    clearTimeout(deadline);
                    if (jobs.get(key)?.controller === controller) jobs.delete(key);
                });
            job = { promise, controller, subscribers: 0 };
            jobs.set(key, job);
        }
        const current = job;
        current.subscribers++;
        return new Promise<string>((resolve, reject) => {
            let settled = false;
            const finish = (error?: unknown, url?: string) => {
                if (settled) return;
                settled = true;
                signal.removeEventListener("abort", cancel);
                current.subscribers--;
                if (!current.subscribers) {
                    if (jobs.get(key) === current) jobs.delete(key);
                    current.controller.abort(aborted());
                }
                if (error) reject(error);
                else if (deps.scope() !== scope) reject(aborted());
                else resolve(url!);
            };
            const cancel = () => finish(signal.reason || aborted());
            signal.addEventListener("abort", cancel, { once: true });
            current.promise.then(
                (url) => finish(undefined, url),
                (error) => finish(error),
            );
            if (signal.aborted) cancel();
        });
    }

    return {
        resolve,
        cached: (id: string) => ready.get(keyFor(id)) || "",
        invalidate: (id: string) => {
            ready.delete(keyFor(id));
        },
    };
}

function waitForPlaybackPoll(signal: AbortSignal) {
    return new Promise<void>((resolve, reject) => {
        const cancel = () => {
            clearTimeout(timer);
            reject(signal.reason || aborted());
        };
        const timer = setTimeout(() => {
            signal.removeEventListener("abort", cancel);
            resolve();
        }, 2500);
        signal.addEventListener("abort", cancel, { once: true });
        if (signal.aborted) cancel();
    });
}

const resolver = createVideoPlaybackResolver({
    request: requestResourcePlayback,
    refresh: refreshResource,
    scope: getActiveUserScope,
    variantUrl: playbackVariantUrl,
    sleep: waitForPlaybackPoll,
});

export function cachedVideoPlaybackSource(src: string, storageKey?: string) {
    const id = videoPlaybackResourceId(src, storageKey);
    return id ? resolver.cached(id) || src : src;
}

export function createVideoPlaybackSession(src: string, storageKey: string | undefined, onChange: (state: VideoPlaybackState) => void, playback = resolver, scope = getActiveUserScope) {
    const id = videoPlaybackResourceId(src, storageKey);
    const userScope = scope();
    const controller = new AbortController();
    const cached = id ? playback.cached(id) : "";
    let state: VideoPlaybackState = { src: cached || src, phase: cached ? "compatible" : "original", message: "" };
    const update = (next: VideoPlaybackState) => {
        if (controller.signal.aborted || scope() !== userScope) return;
        state = next;
        onChange(state);
    };
    return {
        snapshot: () => state,
        dispose: () => controller.abort(aborted()),
        handleError(code?: number) {
            if (controller.signal.aborted || scope() !== userScope || state.phase === "preparing" || state.phase === "failed" || code === 1) return false;
            if (!isVideoDecodeError(code)) {
                update({ ...state, phase: "failed", message: "视频加载失败，请检查网络或重新打开预览。" });
                return false;
            }
            if (state.phase === "compatible") {
                playback.invalidate(id);
                update({ ...state, phase: "failed", message: "兼容预览仍无法播放，可下载原片后用本地播放器观看。" });
                return false;
            }
            if (!id) {
                update({ ...state, phase: "failed", message: "此视频编码无法播放，请先将原片同步到云端后重试。" });
                return false;
            }
            update({ ...state, phase: "preparing", message: preparingMessage });
            void playback.resolve(id, controller.signal).then(
                (url) => update({ src: url, phase: "compatible", message: "" }),
                (error: unknown) => {
                    if (controller.signal.aborted) return;
                    const message =
                        error instanceof ApiError
                            ? error.status === 401 || error.status === 403
                                ? "没有访问视频的权限，请重新登录后重试。"
                                : error.status === 404
                                  ? "视频或转码接口不可用，请确认素材与服务端版本。"
                                  : error.status === 429
                                    ? "兼容预览请求过多，请稍后重试。"
                                    : error.message
                            : error instanceof Error
                              ? error.message
                              : failedMessage;
                    update({ ...state, phase: "failed", message });
                },
            );
            return true;
        },
    };
}
