const passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-";

export function generateAdminPassword(): string {
    const bytes = crypto.getRandomValues(new Uint8Array(24));
    return Array.from(bytes, (value) => passwordAlphabet[value & 63]).join("");
}

export function adminPasswordError(password: string): string | undefined {
    if (Array.from(password).length < 8) return "密码至少 8 位";
    if (new TextEncoder().encode(password).length > 72) return "密码不能超过 72 字节";
    return undefined;
}
