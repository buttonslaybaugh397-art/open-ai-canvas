import { ImageOff } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { cn } from "@/lib/utils";
import { resourceIdFromStorageKey, resourceIdFromUrl, resourceProxyFileUrl, resourceStorageKey } from "@/services/api/resources";
import { cacheResourceObjectUrl } from "@/services/resource-blob-cache";
import { resolveMediaUrl } from "@/services/file-storage";

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
    preload,
    fallbackStorageKey,
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
    preload?: "none" | "metadata" | "auto";
    fallbackStorageKey?: string;
    onUnavailable?: () => void;
}) {
    const resourceId = resourceIdFromStorageKey(fallbackStorageKey) || resourceIdFromUrl(src);
    // The URL returned with the task is the primary preview source. Do not
    // eagerly replace it with a signed object-storage URL: that URL may not
    // expose CORS headers, and the API URL can also be a CDN/provider URL.
    const [activeSrc, setActiveSrc] = useState(src);
    const [unavailable, setUnavailable] = useState(false);
    const fallbackAttemptedRef = useRef(false);
    const localFallbackAttemptedRef = useRef(false);
    const mediaStorageKey = fallbackStorageKey || (resourceId ? resourceStorageKey(resourceId) : "");
    const proxyFallbackUrl = resourceId ? resourceProxyFileUrl(resourceId) : "";
    const sourceVersionRef = useRef(0);

    useEffect(() => {
        sourceVersionRef.current += 1;
        fallbackAttemptedRef.current = false;
        localFallbackAttemptedRef.current = false;
        setActiveSrc(src);
        setUnavailable(false);
    }, [fallbackStorageKey, src]);

    const handleUnavailable = () => {
        if (activeSrc !== proxyFallbackUrl && resourceId && !fallbackAttemptedRef.current) {
            fallbackAttemptedRef.current = true;
            const version = sourceVersionRef.current;
            // A signed OSS URL is not a safe browser-readable fallback when the
            // bucket omits Access-Control-Allow-Origin. Use the authenticated
            // same-origin proxy instead; it can read OSS server-side and return
            // a valid response to the application.
            if (version === sourceVersionRef.current) setActiveSrc(proxyFallbackUrl);
            return;
        }
        if (activeSrc !== proxyFallbackUrl && proxyFallbackUrl) {
            setActiveSrc(proxyFallbackUrl);
            return;
        }
        if (!localFallbackAttemptedRef.current && mediaStorageKey) {
            localFallbackAttemptedRef.current = true;
            const version = sourceVersionRef.current;
            void (resourceId ? cacheResourceObjectUrl(mediaStorageKey) : resolveMediaUrl(mediaStorageKey))
                .then((localUrl) => {
                    if (version !== sourceVersionRef.current) return;
                    if (localUrl && localUrl !== activeSrc) {
                        setActiveSrc(localUrl);
                        return;
                    }
                    setUnavailable(true);
                    onUnavailable?.();
                })
                .catch(() => {
                    if (version === sourceVersionRef.current) {
                        setUnavailable(true);
                        onUnavailable?.();
                    }
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
        return <video src={activeSrc || undefined} width={width} height={height} muted={!controls} playsInline controls={controls} preload={preload || (controls ? "metadata" : "none")} className={className} onError={handleUnavailable} />;
    }

    return <img src={activeSrc} alt={alt} width={width} height={height} loading={loading} className={className} onError={handleUnavailable} />;
}
