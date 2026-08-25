import { describe, expect, test } from "bun:test";

import { canvasResourceMentionToken } from "../src/lib/canvas/canvas-resource-references";
import { creationAttachmentKind, creationFileAccepted, creationMediaAspectRatio, creationUploadAccept, type CreationAttachment } from "../src/pages/create/creation-assets";
import { buildCreationMentionReferences, limitCreationAttachmentCandidates, limitCreationAttachments, reconcileCreationAttachmentLimit, removeCreationReferenceTokens } from "../src/pages/create/creation-references";

function imageAttachment(id: string): CreationAttachment {
    return {
        id,
        name: `${id}.png`,
        type: "image/png",
        dataUrl: `data:image/png;base64,${id}`,
        previewUrl: `data:image/png;base64,${id}`,
    };
}

function mediaAttachment(id: string, type: string): CreationAttachment {
    return { id, name: id, type, url: `https://example.com/${id}`, storageKey: `resource:${id}`, bytes: 1024, previewUrl: `https://example.com/${id}` };
}

describe("creation references", () => {
    test("removes attachments and prompt tokens beyond the current model limit", () => {
        const attachments = [imageAttachment("first"), imageAttachment("second"), imageAttachment("third")];
        const references = buildCreationMentionReferences([], attachments);
        const result = reconcileCreationAttachmentLimit(attachments, references, 1);
        const prompt = references.map(canvasResourceMentionToken).join(" ");
        const nextPrompt = removeCreationReferenceTokens(prompt, result.removedReferences);

        expect(result.attachments).toEqual([attachments[0]]);
        expect(result.removedReferences.map((reference) => reference.attachmentId)).toEqual(["second", "third"]);
        expect(nextPrompt).toContain(canvasResourceMentionToken(references[0]));
        expect(nextPrompt).not.toContain(canvasResourceMentionToken(references[1]));
        expect(nextPrompt).not.toContain(canvasResourceMentionToken(references[2]));
    });

    test("returns the original attachment list when it is already within the limit", () => {
        const attachments = [imageAttachment("first")];
        const result = reconcileCreationAttachmentLimit(attachments, buildCreationMentionReferences([], attachments), 1);

        expect(result.attachments).toBe(attachments);
        expect(result.removedReferences).toEqual([]);
    });

    test("按后台配置分别限制图片、视频和音频数量", () => {
        const attachments = [imageAttachment("image-1"), imageAttachment("image-2"), mediaAttachment("video-1", "video/mp4"), mediaAttachment("audio-1", "audio/mpeg"), mediaAttachment("video-2", "video/mp4")];

        expect(limitCreationAttachments(attachments, { total: 4, image: 1, video: 2, audio: 1, file: 0 }).map((item) => item.id)).toEqual(["image-1", "video-1", "audio-1", "video-2"]);
    });

    test("被分类上限拒绝的素材不会占用总数名额", () => {
        const attachments = [imageAttachment("unsupported-image"), mediaAttachment("video-1", "video/mp4"), mediaAttachment("video-2", "video/mp4")];

        expect(limitCreationAttachments(attachments, { total: 2, image: 0, video: 2 }).map((item) => item.id)).toEqual(["video-1", "video-2"]);
    });

    test("上传前只保留各分类剩余额度内的文件", () => {
        const current = [imageAttachment("image-1"), mediaAttachment("video-1", "video/mp4")];
        const candidates = [
            { name: "image-2.png", type: "image/png" },
            { name: "video-2.mp4", type: "video/mp4" },
            { name: "audio-1.mp3", type: "audio/mpeg" },
            { name: "audio-2.mp3", type: "audio/mpeg" },
        ];

        expect(limitCreationAttachmentCandidates(current, candidates, { total: 4, image: 1, video: 2, audio: 1 }).map((item) => item.name)).toEqual(["video-2.mp4", "audio-1.mp3"]);
    });

    test("文本创作允许媒体和常用文档，图片创作仍只接受图片", () => {
        expect(creationFileAccepted("text", { name: "story.pdf", type: "application/pdf" })).toBe(true);
        expect(creationFileAccepted("text", { name: "clip.mp4", type: "video/mp4" })).toBe(true);
        expect(creationFileAccepted("image", { name: "story.pdf", type: "application/pdf" })).toBe(false);
        expect(creationUploadAccept("text")).toContain(".docx");
    });

    test("文档附件会作为文本资源参与引用", () => {
        const attachment: CreationAttachment = { id: "document", name: "script.pdf", type: "application/pdf", url: "https://example.com/script.pdf", storageKey: "resource:document", bytes: 1024, previewUrl: "" };
        const [reference] = buildCreationMentionReferences([], [attachment]);

        expect(creationAttachmentKind(attachment)).toBe("file");
        expect(reference.kind).toBe("text");
        expect(reference.label).toBe("文件1");
    });

    test("混合参考媒体按各自类型编号", () => {
        const video: CreationAttachment = { id: "video", name: "clip.mp4", type: "video/mp4", url: "https://example.com/clip.mp4", storageKey: "resource:video", bytes: 1024, previewUrl: "https://example.com/clip.mp4" };
        const image = imageAttachment("image");
        const [imageReference, videoReference] = buildCreationMentionReferences([], [image, video]);

        expect(imageReference.label).toBe("图片1");
        expect(videoReference.label).toBe("视频1");
    });

    test("媒体占位按本次选择的画幅展示并为异常值提供模式回退", () => {
        expect(creationMediaAspectRatio("16:9", "video")).toBe("16 / 9");
        expect(creationMediaAspectRatio("1:1", "image")).toBe("1 / 1");
        expect(creationMediaAspectRatio("1920x1080", "image")).toBe("1920 / 1080");
        expect(creationMediaAspectRatio("auto", "video")).toBe("16 / 9");
        expect(creationMediaAspectRatio("auto", "image")).toBe("1 / 1");
    });
});
