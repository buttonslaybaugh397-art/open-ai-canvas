import { buildSkillMentionReferences, renderSkillPrompt } from "@/lib/canvas/canvas-skill-mentions";
import { canvasResourceMentionToken, type CanvasResourceReference } from "@/lib/canvas/canvas-resource-references";
import type { Skill } from "@/services/api/skills";
import { creationAttachmentKind, type CreationAttachment } from "./creation-assets";

export type CreationReference = CanvasResourceReference & {
    attachmentId?: string;
};

export type CreationAttachmentLimits = {
    total?: number;
    image?: number;
    video?: number;
    audio?: number;
    file?: number;
};

export function buildCreationMentionReferences(skills: Skill[], attachments: CreationAttachment[] = [], snapshots: CreationReference[] = []) {
    const attachmentCounts = { image: 0, video: 0, audio: 0, file: 0 };
    const attachmentReferences = attachments.map((attachment) => {
        const kind = creationAttachmentKind(attachment);
        const index = attachmentCounts[kind]++;
        return attachmentReference(attachment, index);
    });
    const skillReferences = buildSkillMentionReferences(skills) as CreationReference[];
    const current = [...attachmentReferences, ...skillReferences];
    const currentIDs = new Set(current.map((reference) => reference.id));
    const restored = snapshots.filter((reference) => reference.kind === "skill" && !currentIDs.has(reference.id));
    return [...current, ...restored].map((reference) => ({ ...reference, active: true }));
}

export function selectedCreationReferences(prompt: string, references: CreationReference[]) {
    return references.filter((reference) => prompt.includes(canvasResourceMentionToken(reference)));
}

export function limitCreationAttachments(attachments: CreationAttachment[], limits: number | CreationAttachmentLimits) {
    const normalized = typeof limits === "number" ? { total: limits } : limits;
    const totalLimit = normalizeAttachmentLimit(normalized.total);
    const counts = { image: 0, video: 0, audio: 0, file: 0 };
    let retainedCount = 0;
    return attachments.filter((attachment) => {
        if (retainedCount >= totalLimit) return false;
        const kind = creationAttachmentKind(attachment);
        const kindLimit = normalizeAttachmentLimit(normalized[kind]);
        if (counts[kind] >= kindLimit) return false;
        counts[kind] += 1;
        retainedCount += 1;
        return true;
    });
}

export function limitCreationAttachmentCandidates<T extends Pick<CreationAttachment, "type">>(attachments: CreationAttachment[], candidates: T[], limits: number | CreationAttachmentLimits) {
    const retainedAttachments = limitCreationAttachments(attachments, limits);
    const normalized = typeof limits === "number" ? { total: limits } : limits;
    const totalLimit = normalizeAttachmentLimit(normalized.total);
    const counts = { image: 0, video: 0, audio: 0, file: 0 };
    retainedAttachments.forEach((attachment) => {
        counts[creationAttachmentKind(attachment)] += 1;
    });
    let retainedCount = retainedAttachments.length;
    return candidates.filter((candidate) => {
        if (retainedCount >= totalLimit) return false;
        const kind = creationAttachmentKind(candidate);
        const kindLimit = normalizeAttachmentLimit(normalized[kind]);
        if (counts[kind] >= kindLimit) return false;
        counts[kind] += 1;
        retainedCount += 1;
        return true;
    });
}

export function reconcileCreationAttachmentLimit(attachments: CreationAttachment[], references: CreationReference[], limits: number | CreationAttachmentLimits) {
    const nextAttachments = limitCreationAttachments(attachments, limits);
    if (nextAttachments.length === attachments.length) return { attachments, removedReferences: [] as CreationReference[] };

    const retained = new Set(nextAttachments);
    const removedAttachmentIds = new Set(attachments.filter((attachment) => !retained.has(attachment)).map((attachment) => attachment.id));
    const removedReferences = references.filter((reference) => reference.attachmentId && removedAttachmentIds.has(reference.attachmentId));
    return { attachments: nextAttachments, removedReferences };
}

function normalizeAttachmentLimit(value: number | undefined) {
    if (value === undefined) return Number.POSITIVE_INFINITY;
    return Math.max(0, Math.floor(value));
}

export function removeCreationReferenceTokens(value: string, references: CreationReference[]) {
    return references.reduce((current, reference) => current.split(canvasResourceMentionToken(reference)).join(""), value);
}

export function displayCreationPrompt(prompt: string, references: CreationReference[]) {
    return references.reduce((value, reference) => value.split(canvasResourceMentionToken(reference)).join(`@${reference.label}`), prompt);
}

export function expandCreationPrompt(prompt: string, references: CreationReference[], attachments: CreationAttachment[] = []) {
    const visiblePrompt = displayCreationPrompt(prompt, references).trim();
    if (!references.length) return visiblePrompt;

    const contexts: string[] = [];
    const mediaMappings: string[] = [];
    const attachmentPositions = new Map<string, number>();
    const attachmentCounts = { image: 0, video: 0, audio: 0, file: 0 };
    attachments.forEach((attachment) => {
        const kind = creationAttachmentKind(attachment);
        attachmentPositions.set(attachment.id, ++attachmentCounts[kind]);
    });
    references.forEach((reference) => {
        if (reference.kind === "skill" && reference.skill) {
            contexts.push(renderSkillPrompt(reference.skill));
            return;
        }
        if (reference.attachmentId) {
            const position = attachmentPositions.get(reference.attachmentId);
            const kindLabel = reference.kind === "video" ? "视频" : reference.kind === "audio" ? "音频" : reference.kind === "text" ? "文件" : "图片";
            mediaMappings.push(`- @${reference.label}：参考${kindLabel} ${position || 1}`);
            return;
        }
    });

    if (mediaMappings.length) contexts.push(`【资源对应关系】\n${mediaMappings.join("\n")}`);
    return [...contexts, `【创作要求】\n${visiblePrompt}`].filter(Boolean).join("\n\n");
}

export function creationReferenceMetadata(references: CreationReference[]) {
    return {
        skillIds: references.flatMap((reference) => (reference.skill?.skill_id ? [reference.skill.skill_id] : [])),
    };
}

function attachmentReference(attachment: CreationAttachment, index: number): CreationReference {
    const kind = creationAttachmentKind(attachment);
    const label = kind === "video" ? "视频" : kind === "audio" ? "音频" : kind === "file" ? "文件" : "图片";
    return {
        id: `upload:${attachment.id}`,
        nodeId: `upload:${attachment.id}`,
        kind: kind === "file" ? "text" : kind,
        label: `${label}${index + 1}`,
        title: "当前参考内容",
        previewUrl: attachment.previewUrl || ("dataUrl" in attachment ? attachment.dataUrl : attachment.url),
        storageKey: attachment.storageKey,
        active: true,
        attachmentId: attachment.id,
    };
}
