export type PromptEditorBounds = {
    min: number;
    max: number;
};

export function clampPromptHeight(height: number, bounds: PromptEditorBounds) {
    if (!Number.isFinite(height)) return bounds.min;
    return Math.round(Math.min(bounds.max, Math.max(bounds.min, height)));
}

export function reconcilePromptHeight(
    height: number | null,
    previousInset: number,
    nextInset: number,
    bounds: PromptEditorBounds,
) {
    if (height === null) return null;
    return clampPromptHeight(height - previousInset + nextInset, bounds);
}
