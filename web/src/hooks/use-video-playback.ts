import { useCallback, useEffect, useRef, useState } from "react";
import { getActiveUserScope } from "@/lib/user-scope";
import { cachedVideoPlaybackSource, createVideoPlaybackSession, type VideoPlaybackState } from "@/services/video-playback";

export function useVideoPlayback(src: string, storageKey?: string) {
    const scope = getActiveUserScope();
    const identity = JSON.stringify([scope, src, storageKey]);
    const sessionRef = useRef<{ identity: string; session: ReturnType<typeof createVideoPlaybackSession> } | null>(null);
    const [snapshot, setSnapshot] = useState<{ identity: string; state: VideoPlaybackState } | null>(null);
    useEffect(() => {
        const session = createVideoPlaybackSession(src, storageKey, (state) => setSnapshot({ identity, state }));
        sessionRef.current = { identity, session };
        setSnapshot({ identity, state: session.snapshot() });
        return () => {
            session.dispose();
            if (sessionRef.current?.session === session) sessionRef.current = null;
        };
    }, [identity, src, storageKey]);
    const onMediaError = useCallback(
        (code?: number) => {
            return sessionRef.current?.identity === identity ? sessionRef.current.session.handleError(code) : false;
        },
        [identity],
    );
    const cached = cachedVideoPlaybackSource(src, storageKey);
    const state: VideoPlaybackState =
        snapshot?.identity === identity
            ? snapshot.state
            : {
                  src: cached,
                  phase: cached !== src ? "compatible" : "original",
                  message: "",
              };
    return { ...state, onMediaError };
}
