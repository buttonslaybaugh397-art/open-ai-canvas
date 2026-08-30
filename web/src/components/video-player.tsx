import { useEffect, useMemo, useState } from "react";
import type { ComponentProps } from "react";
import { MediaPlayer, MediaProvider, type VideoMimeType } from "@vidstack/react";
import { DefaultVideoLayout, defaultLayoutIcons, type DefaultLayoutTranslations } from "@vidstack/react/player/layouts/default";
import "@vidstack/react/player/styles/base.css";
import "@vidstack/react/player/styles/default/theme.css";
import "@vidstack/react/player/styles/default/layouts/video.css";
import "./video-player.css";
import { resourceIdFromStorageKey, resourceIdFromUrl, resourceProxyFileUrl } from "@/services/api/resources";

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

const supportedVideoMimeTypes = new Set<VideoMimeType>(["video/mp4", "video/webm", "video/3gp", "video/ogg", "video/avi", "video/mpeg", "video/object"]);

/**
 * 统一视频播放表面，保留原生媒体 URL 契约，同时提供可访问的完整控件布局。
 * 画布节点需要隔离播放器手势，避免拖动进度条时被误判为拖动画布。
 */
export function VideoPlayer({ src, mimeType, title = "视频", className, brandColor = "#f5f5f5", preload = "metadata", autoPlay = false, dataCanvasNoZoom = false, compactControls = false, fallbackStorageKey, onCanPlay }: VideoPlayerProps) {
    const stopCanvasControlInteraction = (event: { target: EventTarget | null; stopPropagation: () => void }) => {
        if (!dataCanvasNoZoom || !(event.target instanceof Element)) return;
        if (event.target.closest(".vds-controls,.vds-menu-items")) event.stopPropagation();
    };
    const type = mimeType && supportedVideoMimeTypes.has(mimeType as VideoMimeType) ? (mimeType as VideoMimeType) : "video/mp4";
    const resourceId = resourceIdFromStorageKey(fallbackStorageKey) || resourceIdFromUrl(src);
    const proxyFallbackUrl = resourceId ? resourceProxyFileUrl(resourceId) : "";
    const [activeSrc, setActiveSrc] = useState(src);
    const mediaSource = useMemo(() => ({ src: activeSrc, type }), [activeSrc, type]);

    useEffect(() => {
        setActiveSrc(src);
    }, [src]);

    const handleError: MediaPlayerProps["onError"] = (event) => {
        // Keep the provider/API URL as the normal playback path. Only a failed
        // source falls back to the authenticated proxy, which can read a private
        // object even when the bucket does not grant browser CORS access.
        if (proxyFallbackUrl && activeSrc !== proxyFallbackUrl) setActiveSrc(proxyFallbackUrl);
        return event;
    };

    return (
        <MediaPlayer
            className={`canvas-video-player ${compactControls ? "canvas-video-player-compact" : ""} ${className || ""}`}
            src={mediaSource}
            title={title}
            viewType="video"
            streamType="on-demand"
            crossOrigin={null}
            playsInline
            autoPlay={autoPlay}
            load="eager"
            preload={preload}
            data-canvas-no-zoom={dataCanvasNoZoom ? "true" : undefined}
            style={{ "--video-brand": brandColor }}
            onCanPlay={onCanPlay}
            onError={handleError}
            onPointerDown={stopCanvasControlInteraction}
            onMouseDown={stopCanvasControlInteraction}
        >
            <MediaProvider />
            <DefaultVideoLayout icons={defaultLayoutIcons} translations={zhCNTranslations} />
        </MediaPlayer>
    );
}
