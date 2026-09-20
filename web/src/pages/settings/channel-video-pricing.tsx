import { useEffect, useState } from "react";
import { App, Button, Segmented, Tag } from "antd";
import { ChevronRight, FlaskConical, Settings2 } from "lucide-react";

import { ModelEditorModal } from "@/components/model-editor-modal";
import { ModelProtocolBrowser } from "@/components/model-protocol-browser";
import { testChannelModelConnection } from "@/lib/model-connection-test";
import { ModelCapabilityEditor } from "@/components/model-capability-editor";
import { type ModelCapabilityChoice } from "@/components/model-protocol-picker";
import type { ModelProtocolDefinition } from "@/lib/model-protocols";
import { fetchPluginProviderCatalog } from "@/services/api/plugin-catalog";
import { modelOptionName, type ModelChannel } from "@/stores/use-config-store";
import { resolveChannelModelSettings, updateChannelModelSettings } from "./channel-model-settings";

type ModelCost = NonNullable<ModelChannel["modelCosts"]>[number];

export function ChannelModelSettings({ channel, onChange }: { channel: ModelChannel; onChange: (costs: ModelCost[]) => void }) {
    const { message } = App.useApp();
    const [testingModel, setTestingModel] = useState("");
    const [editorTab, setEditorTab] = useState("protocol");
    const [protocolLoading, setProtocolLoading] = useState(true);
    const [protocolError, setProtocolError] = useState("");
    const [activeModel, setActiveModel] = useState<string | null>(null);
    const [availableProtocols, setAvailableProtocols] = useState<ModelProtocolDefinition[]>([]);

    useEffect(() => {
        let active = true;
        void fetchPluginProviderCatalog("user.custom-channel").then((items) => { if (active) setAvailableProtocols(items); })
            .catch((error) => { if (active) setProtocolError(error instanceof Error ? error.message : "协议目录读取失败"); })
            .finally(() => { if (active) setProtocolLoading(false); });
        return () => { active = false; };
    }, []);

    if (!channel.models.length) return null;

    const updateCost = (model: string, patch: Parameters<typeof updateChannelModelSettings>[2]) => {
        try {
            onChange(updateChannelModelSettings(channel, model, patch, availableProtocols));
        } catch (error) {
            message.error(error instanceof Error ? error.message : "模型配置更新失败");
        }
    };

    const testModel = async (model: string) => {
        const { capability, availableProtocol } = resolveChannelModelSettings(channel, model, availableProtocols);
        if (!channel.models.includes(model) || protocolLoading || protocolError || !availableProtocol) return;
        setTestingModel(model);
        try {
            const detail = await testChannelModelConnection(channel, model, capability, availableProtocol.value);
            message.success(`模型测试通过：${detail}`);
        } catch (error) {
            message.error(error instanceof Error ? error.message : "模型测试失败");
        } finally {
            setTestingModel("");
        }
    };

    const activeSettings = resolveChannelModelSettings(channel, activeModel || "", availableProtocols);
    const { protocol: activeProtocol, capability: activeCapability } = activeSettings;
    const canTest = Boolean(activeModel && channel.models.includes(activeModel) && activeSettings.availableProtocol && !protocolLoading && !protocolError);

    return (
        <div className="mt-4">
            <div className="mb-2 flex items-center justify-between gap-3">
                <div>
                    <div className="text-xs font-medium">模型能力与请求协议</div>
                    <div className="mt-0.5 text-[var(--fs-tiny)] text-foreground/42">与运营后台使用同一能力目录；测试会发起真实请求并可能产生供应商费用</div>
                </div>
                <span className="text-[var(--fs-tiny)] text-foreground/35">{channel.models.length} 个模型</span>
            </div>
            <div className="space-y-2">
                {channel.models.map((rawModel) => {
                    const model = modelOptionName(rawModel);
                    const { cost, protocol, capability, availableProtocol } = resolveChannelModelSettings(channel, model, availableProtocols);
                    const protocolStatus = !protocol ? "待配置请求协议" : protocolLoading ? "正在读取协议…" : protocolError ? "协议目录读取失败" : availableProtocol ? availableProtocol.create || availableProtocol.label : `协议不可用：${protocol}`;
                    const displayName = cost?.displayName?.trim() || model;
                    return (
                        <div key={model} className="flex min-w-0 items-center gap-3 rounded-md bg-surface-active px-3 py-2.5 transition-colors hover:bg-surface-hover">
                            <span className="grid size-8 shrink-0 place-items-center rounded-md bg-foreground/[.045] text-foreground/65">
                                <Settings2 className="size-4" />
                            </span>
                            <div className="min-w-0 flex-1">
                                <div className="truncate text-xs font-medium" title={displayName === model ? model : `${displayName} (${model})`}>
                                    {displayName}
                                </div>
                                <div className="mt-1 flex min-w-0 flex-wrap items-center gap-1.5">
                                    <Tag className="mr-0 text-[var(--fs-tiny)]" bordered={false}>
                                        {capabilityLabel(capability)}
                                    </Tag>
                                    <span className="truncate font-mono text-[var(--fs-tiny)] text-foreground/40" title={protocolStatus}>
                                        {protocolStatus}
                                    </span>
                                </div>
                            </div>
                            <Button type="text" size="small" icon={<ChevronRight className="size-4" />} iconPosition="end" onClick={() => { setEditorTab("protocol"); setActiveModel(model); }}>
                                配置使用
                            </Button>
                        </div>
                    );
                })}
            </div>
            <ModelEditorModal
                open={Boolean(activeModel)}
                busy={Boolean(testingModel)}
                title="编辑模型使用配置"
                subtitle={activeModel || ""}
                activeKey={editorTab}
                onTabChange={setEditorTab}
                onClose={() => setActiveModel(null)}
                footer={
                    <div className="model-editor-footer">
                        <span className="text-xs text-foreground/50">更改实时保存到云端渠道配置</span>
                        <div className="model-editor-footer-actions">
                            <Button
                                icon={<FlaskConical className="size-4" />}
                                loading={Boolean(testingModel)}
                                disabled={!canTest}
                                onClick={() => { if (canTest && activeModel) void testModel(activeModel); }}
                            >
                                测试模型
                            </Button>
                            <Button type="primary" disabled={Boolean(testingModel)} onClick={() => setActiveModel(null)}>完成</Button>
                        </div>
                    </div>
                }
                items={activeModel ? [
                    {
                        key: "protocol",
                        label: "基本信息",
                        children: <div className="space-y-4" inert={Boolean(testingModel)}>
                            <section className="space-y-2">
                                <div className="text-xs font-medium">模型能力</div>
                                <Segmented<ModelCapabilityChoice>
                                    block
                                    options={[{ label: "文本", value: "text" }, { label: "图片", value: "image" }, { label: "视频", value: "video" }, { label: "音频", value: "audio" }]}
                                    value={activeCapability}
                                    onChange={(capability) => updateCost(activeModel, { capability })}
                                />
                            </section>
                            <section className="space-y-2">
                                <div className="text-xs font-medium">请求协议</div>
                                <ModelProtocolBrowser
                                    loading={protocolLoading}
                                    error={protocolError}
                                    capability={activeCapability}
                                    value={activeProtocol}
                                    protocols={availableProtocols}
                                    onChange={(protocol) => updateCost(activeModel, { protocol })}
                                />
                            </section>
                        </div>,
                    },
                    {
                        key: "capabilities",
                        label: "能力与参数",
                        children: <div inert={Boolean(testingModel)}>
                            {activeCapability === "text" || activeCapability === "image" || activeCapability === "video" ? (
                                <ModelCapabilityEditor
                                    capability={activeCapability}
                                    model={activeModel}
                                    value={activeSettings.capabilityConfig}
                                    protocol={activeProtocol}
                                    onChange={(capabilityConfig) => updateCost(activeModel, { capabilityConfig })}
                                />
                            ) : <p className="text-xs text-foreground/50">当前模型类型无需额外配置引用与参数。</p>}
                        </div>,
                    },
                ] : []}
            />
        </div>
    );
}

function capabilityLabel(value: ModelCost["capability"]) {
    return { text: "文本", image: "图片", video: "视频", audio: "音频", "": "待配置" }[value] || "待配置";
}
