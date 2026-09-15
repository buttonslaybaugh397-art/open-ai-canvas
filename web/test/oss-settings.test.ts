import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";

import { changesRequireOSSRetest, DEFAULT_OSS_PATH_PREFIX, getS3PresetHints, normalizeOSSConnectionTestInput, S3_PRESET_OPTIONS } from "../src/lib/oss-settings";

describe("OSS settings helpers", () => {
    test("admin response validation accepts every selectable S3 preset, including Rainyun", () => {
        const source = readFileSync(new URL("../src/pages/admin/settings/storage-settings-page.tsx", import.meta.url), "utf8");
        const validator = source.match(/function isAdminOSSSetting\(value: unknown\): value is AdminOSSSetting \{([\s\S]*?)\n\}/)?.[1];
        expect(validator).toBeDefined();
        expect(validator).toContain("S3_PRESET_OPTIONS.some((option) => option.value === setting.s3Preset)");
        expect(S3_PRESET_OPTIONS.some((option) => option.value === "rainyun")).toBe(true);
        expect(getS3PresetHints("rainyun")).toMatchObject({
            endpoint: "https://cn-nb1.rains3.com",
            region: "us-east-1",
            pathStyle: true,
        });
    });

    test("provides editable S3 endpoint hints for known presets", () => {
        expect(getS3PresetHints("r2")).toMatchObject({ region: "auto" });
        expect(getS3PresetHints("b2").endpoint).toContain("backblazeb2.com");
    });

    test("only connection fields invalidate a previous test", () => {
        expect(changesRequireOSSRetest({ endpoint: "https://s3.example.com" })).toBe(true);
        expect(changesRequireOSSRetest({ enabled: true })).toBe(false);
        expect(changesRequireOSSRetest({ allowUserS3: true })).toBe(false);
    });

    test("uses the product path prefix by default", () => {
        expect(DEFAULT_OSS_PATH_PREFIX).toBe("open-ai-canvas");
    });

    test("normalizes a Tencent COS test draft when S3-only fields are not mounted", () => {
        const input = normalizeOSSConnectionTestInput({
            provider: "tencent",
            region: " ap-guangzhou ",
            endpoint: " https://cos.ap-guangzhou.myqcloud.com/ ",
            bucket: " example-1250000000 ",
            accessKeyId: " secret-id ",
            accessKeySecret: " secret-key ",
            pathPrefix: " /canvas/ ",
        });

        expect(input).toMatchObject({
            provider: "tencent",
            region: "ap-guangzhou",
            endpoint: "https://cos.ap-guangzhou.myqcloud.com",
            cdnBaseUrl: "",
            bucket: "example-1250000000",
            accessKeyId: "secret-id",
            accessKeySecret: "secret-key",
            sessionToken: "",
            pathPrefix: "canvas",
            s3Preset: "custom",
            pathStyle: false,
        });
    });
});
