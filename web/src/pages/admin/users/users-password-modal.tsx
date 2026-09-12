import { Alert, App, Button, Form, Input, Tooltip } from "antd";
import { Check, Copy, KeyRound, RefreshCw, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { AppModal } from "@/components/ui/product/app-modal/app-modal";
import { updateAdminUser, type AdminUser } from "@/services/api/auth";
import { adminPasswordError, generateAdminPassword } from "./users-password";

type PasswordValues = { password: string; confirmPassword: string };

export function AdminUserPasswordModal({
    user,
    actorId,
    onClose,
    onSelfReset,
}: {
    user: AdminUser | null;
    actorId?: string;
    onClose: () => void;
    onSelfReset: () => void | Promise<void>;
}) {
    const { message, modal } = App.useApp();
    const [form] = Form.useForm<PasswordValues>();
    const [saving, setSaving] = useState(false);
    const [savedPassword, setSavedPassword] = useState("");
    const submitting = useRef(false);
    const editingSelf = user?.id === actorId;

    useEffect(() => {
        form.resetFields();
        setSavedPassword("");
    }, [form, user?.id]);

    const finish = () => {
        form.resetFields();
        setSavedPassword("");
        onClose();
        if (editingSelf) void onSelfReset();
    };

    const close = () => {
        if (submitting.current) return;
        if (savedPassword) {
            finish();
            return;
        }
        if (!form.isFieldsTouched()) {
            onClose();
            return;
        }
        modal.confirm({
            title: "放弃密码修改？",
            content: "新密码尚未保存，原密码仍然有效。",
            okText: "放弃修改",
            cancelText: "继续编辑",
            onOk: () => { form.resetFields(); onClose(); },
        });
    };

    const generate = () => {
        try {
            const password = generateAdminPassword();
            form.setFieldsValue({ password, confirmPassword: password });
            void form.validateFields().catch(() => undefined);
        } catch {
            message.error("无法生成安全随机密码，请手动设置新密码");
        }
    };

    const save = async () => {
        if (!user || submitting.current || savedPassword) return;
        submitting.current = true;
        let values: PasswordValues;
        try {
            values = await form.validateFields();
        } catch {
            submitting.current = false;
            return;
        }
        setSaving(true);
        try {
            await updateAdminUser(user.id, { password: values.password });
            setSavedPassword(values.password);
            form.resetFields();
        } catch (error) {
            message.error(error instanceof Error ? error.message : "密码修改失败");
        } finally {
            submitting.current = false;
            setSaving(false);
        }
    };

    const copy = async () => {
        try {
            await navigator.clipboard.writeText(savedPassword);
            message.success("新密码已复制");
        } catch {
            message.error("复制失败，请显示并手动复制新密码");
        }
    };

    return (
        <AppModal
            title={savedPassword ? "密码已重置" : "修改 / 重置密码"}
            open={Boolean(user)}
            width={480}
            centered
            onCancel={close}
            mask={{ closable: !saving && !savedPassword }}
            keyboard={!saving && !savedPassword}
            closable={!saving && !savedPassword}
            footer={savedPassword ? (
                <Button type="primary" icon={<Check className="size-4" />} onClick={finish}>{editingSelf ? "重新登录" : "完成"}</Button>
            ) : (
                <div className="flex flex-wrap justify-end gap-2">
                    <Button icon={<X className="size-4" />} disabled={saving} onClick={close}>取消</Button>
                    <Button type="primary" icon={<KeyRound className="size-4" />} loading={saving} onClick={() => void save()}>确认重置密码</Button>
                </div>
            )}
        >
            <p className="mb-4 break-all text-sm text-foreground/65">{user?.displayName} @{user?.username}</p>
            {savedPassword ? (
                <div className="space-y-4">
                    <Alert type="success" showIcon title={editingSelf ? "密码已修改，当前会话已失效。" : "新密码已生效，该用户的旧登录会话已撤销。"} />
                    <div>
                        <label htmlFor="admin-saved-password" className="mb-2 block text-sm">新密码</label>
                        <div className="flex min-w-0 gap-2">
                            <Input.Password id="admin-saved-password" value={savedPassword} readOnly autoComplete="off" className="min-w-0 flex-1" />
                            <Tooltip title="复制新密码"><Button aria-label="复制新密码" icon={<Copy className="size-4" />} onClick={() => void copy()} /></Tooltip>
                        </div>
                    </div>
                </div>
            ) : (
                <>
                    <Alert className="mb-4" type="warning" showIcon title={editingSelf ? "保存后将退出当前账号及其他设备。积分和历史数据不变。" : "保存后原密码立即失效，所有设备需重新登录。积分和历史数据不变。"} />
                    <Form form={form} layout="vertical" requiredMark={false} disabled={saving} preserve={false}>
                        <Form.Item name="password" label="新密码" rules={[
                            { required: true, message: "请输入新密码" },
                            { validator: (_, value: string = "") => { const error = adminPasswordError(value); return error ? Promise.reject(new Error(error)) : Promise.resolve(); } },
                        ]}>
                            <Input.Password autoComplete="new-password" maxLength={72} placeholder="至少 8 位" />
                        </Form.Item>
                        <Form.Item name="confirmPassword" label="确认新密码" dependencies={["password"]} rules={[
                            { required: true, message: "请再次输入新密码" },
                            { validator: (_, value) => value === form.getFieldValue("password") ? Promise.resolve() : Promise.reject(new Error("两次输入的密码不一致")) },
                        ]}>
                            <Input.Password autoComplete="new-password" maxLength={72} />
                        </Form.Item>
                        <Button icon={<RefreshCw className="size-4" />} disabled={saving} onClick={generate}>生成随机密码</Button>
                    </Form>
                </>
            )}
        </AppModal>
    );
}
