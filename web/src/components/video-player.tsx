import { useEffect, useMemo, useRef, useState } from "react";
import type { ComponentProps } from "react";
import { isVideoProvider, MediaPlayer, MediaProvider, type MediaPlayerInstance, type VideoMimeType } from "@vidstack/react";
import { DefaultVideoLayout, defaultLayoutIcons, type DefaultLayoutTranslations } from "@vidstack/react/player/layouts/default";
import { detectVideoAudioTrack, detectVideoAudioTrackFromUrl } from "@/lib/video-poster";
import { resourceIdFromStorageKey, resourceIdFromUrl, resourceProxyFileUrl, resourceStorageKey } from "@/services/api/resources";
import { cacheResourceObjectUrl } from "@/services/resource-blob-cache";
import "@vidstack/react/player/styles/base.css";
import "@vidstack/react/player/styles/default/theme.css";
import "@vidstack/react/player/styles/default/layouts/video.css";
import "./video-player.css";

type MediaPlayerProps = ComponentProps<typeof MediaPlayer>;

type VideoPlayerProps = {
    src: string;
    mimeType?: string;
    title?: string;
    className?: string;
    brandColor?: string;
    preload?: MediaPlayerProps["preload"];
    autoPlay?: boolean;
    dataCanvasNoZoom?: boolean;
    compactControls?: boolean;
    /** Explicitly marks videos known to have an audio track (or not). */
    hasAudio?: boolean;
    /** Resource storage key used when the original provider URL cannot play. */
    fallbackStorageKey?: string;
    onCanPlay?: MediaPlayerProps["onCanPlay"];
};

const zhCNTranslations = {
    Accessibility: "辅助功能",
    AirPlay: "隔空播放",
    Audio: "音频",
    Auto: "自动",
    Boost: "音量增强",
    Captions: "字幕",
    "Caption Styles": "字幕样式",
    Chapters: "章节",
    "Closed-Captions Off": "关闭字幕",
    "Closed-Captions On": "开启字幕",
    Connected: "已连接",
    Connecting: "连接中",
    Default: "默认",
    Disabled: "已禁用",
    Disconnected: "已断开",
    Download: "下载",
    "Enter Fullscreen": "进入全屏",
    "Enter PiP": "进入画中画",
    "Exit Fullscreen": "退出全屏",
    "Exit PiP": "退出画中画",
    Fullscreen: "全屏",
    Loop: "循环播放",
    Mute: "静音",
    Normal: "正常",
    Off: "关闭",
    Pause: "暂停",
    Play: "播放",
    Playback: "播放",
    PiP: "画中画",
    Quality: "画质",
    Replay: "重新播放",
    Reset: "重置",
    Seek: "跳转",
    "Seek Backward": "快退",
    "Seek Forward": "快进",
    Settings: "设置",
    Speed: "倍速",
    Unmute: "取消静音",
    Volume: "音量",
} satisfies Partial<DefaultLayoutTranslations>;

function videoMimeType(value: string | undefined, source: string): VideoMimeType {
    const declared = String(value || "")
        .split(";", 1)[0]
        .trim()
        .toLowerCase();
    if (declared.startsWith("video/")) return declared as VideoMimeType;
    const path = source.split(/[?#]/, 1)[0].toLowerCase();
    if (path.endsWith(".webm")) return "video/webm";
    if (path.endsWith(".mov") || path.endsWith(".qt")) return "video/quicktime" as VideoMimeType;
    if (path.endsWith(".mkv")) return "video/x-matroska" as VideoMimeType;
    if (path.endsWith(".avi")) return "video/x-msvideo" as VideoMimeType;
    if (path.endsWith(".ogv") || path.endsWith(".ogg")) return "video/ogg";
    if (path.endsWith(".m4v")) return "video/x-m4v" as VideoMimeType;
    if (path.endsWith(".mpeg") || path.endsWith(".mpg")) return "video/mpeg";
    return "video/mp4";
}

/**
 * 统一视频播放表面，保留原生媒体 URL 契约，同时提供可访问的完整控件布局。
 * 画布节点需要隔离播放器手势，避免拖动进度条时被误判为拖动画布。
 */
export function VideoPlayer({ src, mimeType, title = "视频", className, brandColor = "#f5f5f5", preload = "metadata", autoPlay = false, dataCanvasNoZoom = false, compactControls = false, hasAudio, fallbackStorageKey, onCanPlay }: VideoPlayerProps) {
    const [detectedHasAudio, setDetectedHasAudio] = useState<boolean | undefined>(undefined);
    const autoPlayAttemptedRef = useRef(false);
    const audioProbeGenerationRef = useRef(0);
    const mediaPlayerRef = useRef<MediaPlayerInstance>(null);
    const resourceId = resourceIdFromStorageKey(fallbackStorageKey) || resourceIdFromUrl(src);
    const proxyFallbackUrl = resourceId ? resourceProxyFileUrl(resourceId) : "";
    const [activeSrc, setActiveSrc] = useState(src);
    const fallbackAttemptedRef = useRef(false);
    const cacheFallbackAttemptedRef = useRef(false);
    const sourceVersionRef = useRef(0);
    // Match LibTV's conservative rule: only explicit/container-confirmed
    // silence mutes the player. Runtime probes are used to confirm audio and
    // correct stale persisted `false`, but a negative/unknown probe never
    // forces an otherwise-unknown video into the muted state.
    // A conclusive container probe wins over stale persisted metadata in
    // either direction. Browser runtime probes remain conservative when they
    // cannot expose an audio-track list.
    const effectiveHasAudio = detectedHasAudio === false ? false : hasAudio === true || detectedHasAudio === true ? true : hasAudio === false ? false : undefined;
    const noAudio = effectiveHasAudio === false;
    const layoutIcons = useMemo(() => {
        if (!noAudio) return defaultLayoutIcons;
        // Vidstack chooses the volume icon from its internal media state. That
        // state can briefly lag behind an explicit `hasAudio: false` prop, so
        // keep all volume variants visually muted for known silent videos.
        return {
            ...defaultLayoutIcons,
            MuteButton: {
                ...defaultLayoutIcons.MuteButton,
                VolumeLow: defaultLayoutIcons.MuteButton.Mute,
                VolumeHigh: defaultLayoutIcons.MuteButton.Mute,
            },
        };
    }, [noAudio]);

    useEffect(() => {
        setDetectedHasAudio(undefined);
        autoPlayAttemptedRef.current = false;
        audioProbeGenerationRef.current += 1;
        fallbackAttemptedRef.current = false;
        cacheFallbackAttemptedRef.current = false;
        sourceVersionRef.current += 1;
        setActiveSrc(src);
    }, [fallbackStorageKey, src]);

    useEffect(() => {
        const player = mediaPlayerRef.current?.el;
        if (!player) return;
        // Vidstack exposes these controls as custom elements. Setting their
        // disabled state after layout creation keeps both pointer and keyboard
        // access consistent when an audio-track probe resolves asynchronously.
        const muteButton = player.querySelector<HTMLButtonElement>(".vds-mute-button");
        if (muteButton) {
            muteButton.disabled = noAudio;
            muteButton.setAttribute("aria-disabled", String(noAudio));
        }
        const volumeSlider = player.querySelector<HTMLElement>(".vds-volume-slider");
        if (volumeSlider) {
            (volumeSlider as HTMLElement & { disabled?: boolean }).disabled = noAudio;
            volumeSlider.setAttribute("aria-disabled", String(noAudio));
        }
    }, [noAudio]);

    const probeRemoteAudioTrack = () => {
        if (hasAudio === false) return;
        const generation = audioProbeGenerationRef.current;
        void detectVideoAudioTrackFromUrl(src).then((detected) => {
            if (generation === audioProbeGenerationRef.current && detected !== undefined) setDetectedHasAudio(detected);
        });
    };

    const stopCanvasControlInteraction = (event: { target: EventTarget | null; nativeEvent?: Event; stopPropagation: () => void; preventDefault?: () => void }) => {
        if (!isPlayerControlEvent(event)) return;
        if (noAudio && isVolumeControlEvent(event)) {
            event.preventDefault?.();
            event.stopPropagation();
            return;
        }
        if (dataCanvasNoZoom) event.stopPropagation();
    };
    const stopCanvasControlClick = (event: React.MouseEvent) => {
        if (!isPlayerControlEvent(event)) return;
        if (noAudio && isVolumeControlEvent(event)) {
            event.preventDefault();
            event.stopPropagation();
            return;
        }
        if (dataCanvasNoZoom) event.stopPropagation();
    };
    const type = videoMimeType(mimeType, src);
    const mediaSource = useMemo(() => ({ src: activeSrc, type }), [activeSrc, type]);
    const handleCanPlay = (detail: Parameters<NonNullable<MediaPlayerProps["onCanPlay"]>>[0], event: Parameters<NonNullable<MediaPlayerProps["onCanPlay"]>>[1]) => {
        const provider = event.target.provider;
        const media = isVideoProvider(provider) ? provider.media : undefined;
        const detected = media ? detectVideoAudioTrack(media) : undefined;
        if (detected !== undefined) setDetectedHasAudio(detected);
        else probeRemoteAudioTrack();
        // `canplay` may fire again after buffering or seeking. Only the first
        // event may satisfy the activation autoplay intent; later events must
        // not override a user pause.
        if (autoPlay && !autoPlayAttemptedRef.current) {
            autoPlayAttemptedRef.current = true;
            void event.target.play().catch(() => undefined);
        }
        onCanPlay?.(detail, event);
    };

    const handleError: MediaPlayerProps["onError"] = (event) => {
        // Provider URLs can be expired or blocked by CORS. Retry through the
        // authenticated resource endpoint, then use the cached blob as a final fallback.
        if (proxyFallbackUrl && activeSrc !== proxyFallbackUrl && !fallbackAttemptedRef.current) {
            fallbackAttemptedRef.current = true;
            setActiveSrc(proxyFallbackUrl);
            return event;
        }
        if (resourceId && !cacheFallbackAttemptedRef.current) {
            cacheFallbackAttemptedRef.current = true;
            const version = sourceVersionRef.current;
            void cacheResourceObjectUrl(fallbackStorageKey || resourceStorageKey(resourceId)).then((cachedUrl) => {
                if (version === sourceVersionRef.current && cachedUrl) setActiveSrc(cachedUrl);
            });
        }
        return event;
    };

    return (
        <MediaPlayer
            className={`canvas-video-player ${compactControls ? "canvas-video-player-compact" : ""} ${className || ""}`}
            data-player-variant={compactControls ? "compact" : "dialog"}
            ref={mediaPlayerRef}
            src={mediaSource}
            title={title}
            viewType="video"
            streamType="on-demand"
            playsInline
            autoPlay={autoPlay}
            muted={noAudio ? true : undefined}
            load="eager"
            preload={preload}
            data-canvas-no-zoom={dataCanvasNoZoom ? "true" : undefined}
            data-no-audio={noAudio ? "true" : undefined}
            style={{ "--video-brand": brandColor }}
            onCanPlay={handleCanPlay}
            onError={handleError}
            onLoadedMetadata={(event) => {
                const provider = event.target.provider;
                const media = isVideoProvider(provider) ? provider.media : undefined;
                const detected = media ? detectVideoAudioTrack(media) : undefined;
                if (detected !== undefined) setDetectedHasAudio(detected);
                else probeRemoteAudioTrack();
            }}
            onLoadedData={(event) => {
                const provider = event.target.provider;
                const media = isVideoProvider(provider) ? provider.media : undefined;
                const detected = media ? detectVideoAudioTrack(media) : undefined;
                if (detected !== undefined) setDetectedHasAudio(detected);
                else probeRemoteAudioTrack();
            }}
            onPointerDown={stopCanvasControlInteraction}
            onMouseDown={stopCanvasControlInteraction}
            onClick={stopCanvasControlClick}
            onKeyDown={stopCanvasControlInteraction}
        >
            <MediaProvider />
            <DefaultVideoLayout
                icons={layoutIcons}
                translations={zhCNTranslations}
                // Canvas controls use one stable DOM arrangement regardless
                // of the node's aspect ratio. Without this override Vidstack
                // swaps between its small and large layouts as the node is
                // resized, moving the same icons to different groups.
                smallLayoutWhen={compactControls ? true : undefined}
                // The canvas selects and mounts a player from the node's
                // pointer event. Vidstack's default pointerup gesture can
                // otherwise toggle the freshly mounted player a second time,
                // causing the intermittent play-then-pause-at-zero bug.
                noGestures={dataCanvasNoZoom}
                slots={{
                    // 节点外部已经显示标题，播放器内不再重复渲染章节标题和设置菜单。
                    chapterTitle: null,
                    settingsMenu: null,
                }}
            />
        </MediaPlayer>
    );
}

function isPlayerControlEvent(event: { target: EventTarget | null; nativeEvent?: Event }) {
    if (event.target instanceof Element && event.target.closest(".vds-controls,.vds-menu-items")) return true;
    return event.nativeEvent?.composedPath?.().some((item) => item instanceof Element && Boolean(item.closest(".vds-controls,.vds-menu-items"))) || false;
}

function isVolumeControlEvent(event: { target: EventTarget | null; nativeEvent?: Event }) {
    if (event.target instanceof Element && event.target.closest(".vds-volume,.vds-mute-button,.vds-volume-slider")) return true;
    return event.nativeEvent?.composedPath?.().some((item) => item instanceof Element && Boolean(item.closest(".vds-volume,.vds-mute-button,.vds-volume-slider"))) || false;
}
