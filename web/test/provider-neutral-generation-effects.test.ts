import { expect, test } from "bun:test";

import { createProviderNeutralGenerationTaskEffectStore } from "../src/services/provider-neutral-generation-effects";

test("effect leases remain unique and idempotent without crypto.randomUUID", async () => {
    const descriptor = Object.getOwnPropertyDescriptor(crypto, "randomUUID");
    Object.defineProperty(crypto, "randomUUID", { configurable: true, value: undefined });
    try {
        expect(crypto.randomUUID).toBeUndefined();
        const now = () => new Date("2026-09-21T00:00:00.000Z");
        const owner = createProviderNeutralGenerationTaskEffectStore({ now });
        const observer = createProviderNeutralGenerationTaskEffectStore({ now });
        const taskId = "task-without-random-uuid";
        const firstKey = `materialize:${taskId}:0`;
        const secondKey = `materialize:${taskId}:1`;
        const first = await owner.claim(firstKey, taskId);
        const second = await owner.claim(secondKey, taskId);
        if (first.status !== "claimed" || second.status !== "claimed") throw new Error("Expected both leases to be claimed");
        expect(first.binding).toMatch(/^[A-Za-z0-9_-]{21}$/);
        expect(second.binding).toMatch(/^[A-Za-z0-9_-]{21}$/);
        expect(second.binding).not.toBe(first.binding);
        expect((await observer.claim(firstKey, taskId)).status).toBe("busy");
        expect(await owner.renew(firstKey, taskId, first.binding)).toEqual({ fence: 1 });

        const result = { materializedAssetId: "asset-without-random-uuid" };
        await owner.complete(firstKey, taskId, result, first.binding);
        for (let replay = 0; replay < 3; replay += 1) {
            expect(await observer.claim(firstKey, taskId)).toEqual({ status: "completed", result });
        }

        await owner.release(secondKey, taskId, second.binding);
        const reclaimed = await observer.claim(secondKey, taskId);
        if (reclaimed.status !== "claimed") throw new Error("Expected the released lease to be reclaimed");
        expect(reclaimed.fence).toBe(2);
        expect(reclaimed.binding).not.toBe(second.binding);
        await observer.complete(secondKey, taskId, {}, reclaimed.binding);
    } finally {
        if (descriptor) Object.defineProperty(crypto, "randomUUID", descriptor);
        else Reflect.deleteProperty(crypto, "randomUUID");
    }
});
