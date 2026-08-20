import { createContext, useContext, type ReactNode } from "react";

export const DEFAULT_CANVAS_GENERATION_RATIO = "16:9";
export const CANVAS_GENERATION_RATIO_OPTIONS = ["16:9", "9:16", "1:1", "4:3", "3:4", "21:9"] as const;

const CanvasGenerationRatioContext = createContext<string | undefined>(undefined);

export function isCanvasGenerationRatio(value: unknown): value is (typeof CANVAS_GENERATION_RATIO_OPTIONS)[number] {
    return typeof value === "string" && CANVAS_GENERATION_RATIO_OPTIONS.includes(value.trim() as (typeof CANVAS_GENERATION_RATIO_OPTIONS)[number]);
}

export function normalizeCanvasGenerationRatio(value: unknown, fallback = DEFAULT_CANVAS_GENERATION_RATIO) {
    return isCanvasGenerationRatio(value) ? value.trim() : fallback;
}

export function resolveCanvasGenerationRatio(canvasRatio: unknown, globalRatio: unknown) {
    return normalizeCanvasGenerationRatio(canvasRatio, normalizeCanvasGenerationRatio(globalRatio));
}

export function CanvasGenerationRatioProvider({ value, children }: { value: string; children: ReactNode }) {
    return <CanvasGenerationRatioContext.Provider value={normalizeCanvasGenerationRatio(value)}>{children}</CanvasGenerationRatioContext.Provider>;
}

export function useCanvasGenerationRatio() {
    return useContext(CanvasGenerationRatioContext);
}
