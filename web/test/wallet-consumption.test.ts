import { expect, test } from "bun:test";

test("wallet exposes personal natural-period consumption and daily trend", async () => {
    const [api, page, css] = await Promise.all([
        Bun.file(new URL("../src/services/api/wallet.ts", import.meta.url)).text(),
        Bun.file(new URL("../src/pages/wallet/index.tsx", import.meta.url)).text(),
        Bun.file(new URL("../src/styles/globals.css", import.meta.url)).text(),
    ]);

    for (const field of ["todayMicrocredits", "yesterdayMicrocredits", "weekMicrocredits", "monthMicrocredits", "daily"]) {
        expect(api).toContain(field);
    }
    expect(page).toContain("个人消耗");
    expect(page).toContain("今日消耗");
    expect(page).toContain("昨日消耗");
    expect(page).toContain("本周消耗");
    expect(page).toContain("本月消耗");
    expect(page).toContain("本月个人每日积分消耗图");
    expect(page).toContain("冻结和退款不计入消耗");
    expect(css).toContain(".wallet-consumption-metrics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr))");
    expect(css).toContain(".wallet-consumption-metrics { grid-template-columns: minmax(0, 1fr); }");
    expect(css).toContain(".wallet-consumption-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }");
});
