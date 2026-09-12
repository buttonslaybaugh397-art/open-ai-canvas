import { describe, expect, test } from "bun:test";
import { clampPromptHeight, reconcilePromptHeight } from "../src/lib/canvas/canvas-prompt-height";

describe("canvas prompt height limits", () => {
    const bounds = { min: 44, max: 332 };

    test("clamps pointer and keyboard heights to stable integer bounds", () => {
        expect(clampPromptHeight(12, bounds)).toBe(44);
        expect(clampPromptHeight(381, bounds)).toBe(332);
        expect(clampPromptHeight(91.7, bounds)).toBe(92);
        expect(clampPromptHeight(Number.NaN, bounds)).toBe(44);
    });

    test("preserves the editable area when the reference shelf changes", () => {
        expect(reconcilePromptHeight(100, 0, 58, { min: 102, max: 390 })).toBe(158);
        expect(reconcilePromptHeight(158, 58, 0, bounds)).toBe(100);
        expect(reconcilePromptHeight(380, 0, 58, { min: 102, max: 390 })).toBe(390);
        expect(reconcilePromptHeight(null, 0, 58, { min: 102, max: 390 })).toBeNull();
    });
});
