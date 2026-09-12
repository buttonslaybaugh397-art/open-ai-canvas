import { App, Button, Form, Input } from "antd";
import { Pencil, Save, Undo2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { updateOwnDisplayName, type UpdateOwnDisplayNameInput } from "@/services/api/auth";
import { useUserStore, type LocalUser } from "@/stores/use-user-store";

export function AccountSettingsPane() {
    const user = useUserStore((state) => state.user);
    return user ? <AccountDisplayNameForm key={user.id} user={user} /> : null;
}

function AccountDisplayNameForm({ user }: { user: LocalUser }) {
    const { message } = App.useApp();
    const [form] = Form.useForm<UpdateOwnDisplayNameInput>();
    const [saving, setSaving] = useState(false);
    const submitting = useRef(false);
    const displayName = Form.useWatch("displayName", form) ?? user.displayName;
    const changed = displayName.trim() !== user.displayName;

    useEffect(() => {
        form.setFieldsValue({ displayName: user.displayName });
    }, [form, user.displayName]);

    const save = async (values: UpdateOwnDisplayNameInput) => {
        if (submitting.current || !changed || useUserStore.getState().user?.id !== user.id) return;
        submitting.current = true;
        setSaving(true);
        try {
            const result = await updateOwnDisplayName({ displayName: values.displayName.trim() });
            if (useUserStore.getState().user?.id !== user.id) return;
            useUserStore.getState().updateDisplayName(result.user);
            form.setFieldsValue({ displayName: result.user.displayName });
            message.success("显示名称已修改");
        } catch (error) {
            if (useUserStore.getState().user?.id !== user.id) return;
            message.error(error instanceof Error ? error.message : "修改显示名称失败");
        } finally {
            submitting.current = false;
            setSaving(false);
        }
    };

    return (
        <>
            <div className="settings-pane-header">
                <h2>账号设置</h2>
            </div>
            <section className="settings-section">
                <div className="w-full max-w-lg py-4">
                    <dl className="mb-6 grid grid-cols-[80px_minmax(0,1fr)] gap-x-4 gap-y-3 text-sm">
                        <dt className="text-foreground/55">登录用户名</dt>
                        <dd className="m-0 break-all">{user.username}</dd>
                        <dt className="text-foreground/55">邮箱</dt>
                        <dd className="m-0 break-all">{user.email || "未绑定"}</dd>
                    </dl>
                    <Form form={form} layout="vertical" initialValues={{ displayName: user.displayName }} onFinish={save} disabled={saving} requiredMark={false}>
                        <Form.Item
                            name="displayName"
                            label="显示名称"
                            validateFirst
                            rules={[
                                { required: true, whitespace: true, message: "请输入显示名称" },
                                {
                                    validator: (_, value: string) => {
                                        const name = value.trim();
                                        if (Array.from(name).length > 40) return Promise.reject(new Error("显示名称最多 40 个字符"));
                                        if (/[\p{Cc}\u2028\u2029]/u.test(name)) return Promise.reject(new Error("显示名称不能包含换行或控制字符"));
                                        return Promise.resolve();
                                    },
                                },
                            ]}
                        >
                            <Input prefix={<Pencil className="size-3.5 text-foreground/45" aria-hidden />} autoComplete="nickname" spellCheck={false} />
                        </Form.Item>
                        <div className="flex flex-wrap items-center gap-2">
                            <Button type="primary" htmlType="submit" icon={<Save className="size-4" />} loading={saving} disabled={!changed}>
                                保存
                            </Button>
                            <Button icon={<Undo2 className="size-4" />} disabled={!changed || saving} onClick={() => form.setFieldsValue({ displayName: user.displayName })}>
                                取消修改
                            </Button>
                        </div>
                    </Form>
                </div>
            </section>
        </>
    );
}
