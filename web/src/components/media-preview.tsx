import { ImageOff } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { getResolvedVideoFallbackUrl, setResolvedVideoFallbackUrl, useResolvedVideoFallbackUrl } from "@/lib/task-media";
import { cn } from "@/lib/utils";
import { getResourceOSSUrl } from "@/services/api/resources";
import { resourceIdFromStorageKey } from "@/services/api/resources";

const DEFAULT_UNAVAILABLE_LABEL = "预览不可用，素材可能已删除";

export function MediaPreview({
    src,
    kind,
    alt = "",
    className,
    fallbackClassName,
    fallbackLabel = DEFAULT_UNAVAILABLE_LABEL,
    controls = false,
    loading,
    width,
    height,
    fallbackStorageKey,
    resolvedFallbackUrl,
    onFallbackResolved,
    onUnavailable,
}: {
    src: string;
    kind: "image" | "video";
    alt?: string;
    className?: string;
    fallbackClassName?: string;
    fallbackLabel?: string;
    controls?: boolean;
    loading?: "eager" | "lazy";
    width?: number;
    height?: number;
    fallbackStorageKey?: string;
    resolvedFallbackUrl?: string;
    onFallbackResolved?: (url: string) => void;
    onUnavailable?: () => void;
}) {
    const cachedFallbackUrl = useResolvedVideoFallbackUrl(kind === "video" ? src : "");
    const knownFallbackUrl = resolvedFallbackUrl || cachedFallbackUrl || getResolvedVideoFallbackUrl(src);
    const [activeSrc, setActiveSrc] = useState(knownFallbackUrl || src);
    const [unavailable, setUnavailable] = useState(false);
    const fallbackAttemptedRef = useRef(false);
    const sourceVersionRef = useRef(0);

    useEffect(() => {
        sourceVersionRef.current += 1;
        fallbackAttemptedRef.current = Boolean(knownFallbackUrl);
        setActiveSrc(knownFallbackUrl || src);
        setUnavailable(false);
    }, [fallbackStorageKey, knownFallbackUrl, src]);

    const handleUnavailable = () => {
        if (kind === "video" && activeSrc === src && resourceIdFromStorageKey(fallbackStorageKey) && !fallbackAttemptedRef.current) {
            fallbackAttemptedRef.current = true;
            const version = sourceVersionRef.current;
            void getResourceOSSUrl(fallbackStorageKey).then((fallbackUrl) => {
                if (version !== sourceVersionRef.current) return;
                if (!fallbackUrl || fallbackUrl === src) {
                    setUnavailable(true);
                    onUnavailable?.();
                    return;
                }
                setResolvedVideoFallbackUrl(src, fallbackUrl);
                setActiveSrc(fallbackUrl);
                onFallbackResolved?.(fallbackUrl);
            }).catch(() => {
                if (version !== sourceVersionRef.current) return;
                setUnavailable(true);
                onUnavailable?.();
            });
            return;
        }
        setUnavailable(true);
        onUnavailable?.();
    };

    if (unavailable) {
        return (
            <span className={cn("media-unavailable", fallbackClassName)} role="img" aria-label={fallbackLabel} title={fallbackLabel}>
                <ImageOff aria-hidden="true" />
                <span>{fallbackLabel}</span>
            </span>
        );
    }

    if (kind === "video") {
        return <video src={activeSrc} width={width} height={height} muted={!controls} playsInline controls={controls} preload="metadata" className={className} onError={handleUnavailable} />;
    }

    return <img src={src} alt={alt} width={width} height={height} loading={loading} className={className} onError={handleUnavailable} />;
}
