import { test, expect, loginAdmin } from "./fixture";
import { placeTestOrder } from "./checkout";
import type { Donation, Product } from "../src/api";

test("个人捐赠记录独立保存、导入和清除，管理员仍可查看个人记录", async ({
  page,
  request,
  browser,
  context,
}) => {
  const base = process.env.HASHDROP_TEST_URL!;
  const login = await request.post(`${base}/api/admin/login`, {
    headers: { Origin: base },
    data: { username: "admin", password: "test-password-only" },
  });
  expect(login.ok()).toBeTruthy();
  const headers = { Origin: base, "X-CSRF-Token": (await login.json()).csrf };
  const original = await (
    await request.get(`${base}/api/admin/donation`)
  ).json();
  expect(
    (
      await request.put(`${base}/api/admin/donation`, {
        headers,
        data: { ...original, enabled: true },
      })
    ).ok(),
  ).toBeTruthy();
  try {
    const products: Product[] = await (
      await request.get(`${base}/api/products`)
    ).json();
    const product = products.find((item) => item.kind === "static")!;
    await page.goto(`${base}/products/${product.id}`);
    await expect(
      page
        .locator(".topbar")
        .getByRole("button", { name: "个人中心", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "下单", exact: true }).click();
    await placeTestOrder(page);
    await page.goto(`${base}/donate`);
    await expect(
      page
        .locator(".topbar")
        .getByRole("button", { name: "个人中心", exact: true }),
    ).toBeVisible();
    const created = page.waitForResponse(
      (res) =>
        res.url() === `${base}/api/donations` &&
        res.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "确认金额并赞赏", exact: true })
      .click();
    const { donation, token } = (await (await created).json()) as {
      donation: Donation;
      token: string;
    };
    await expect(page.locator("#donation-receipt-link")).toBeVisible();
    const receipt = await page.locator("#donation-receipt-link").inputValue();
    await expect(
      page.getByRole("button", { name: "前往 HashPay 付款", exact: true }),
    ).toBeEnabled({ timeout: 30_000 });
    expect(
      (await request.post(`${base}/__test/pay-donation/${donation.id}`)).ok(),
    ).toBeTruthy();
    await page.goto(`${base}/center/donations`);
    await expect(page.locator(".donation-history-row")).toHaveCount(1);
    await page.reload();
    await expect(page.locator(".donation-history-row")).toContainText(
      donation.id,
    );
    await expect(page.locator(".donation-history-row")).toContainText("已付款");
    await expect(
      page
        .locator(".topbar")
        .getByRole("button", { name: "个人中心", exact: true }),
    ).toHaveCount(0);
    await loginAdmin(page);
    await expect(
      page.getByRole("menuitem", { name: "商品", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("menuitem", { name: "捐赠记录", exact: true }),
    ).toBeVisible();
    await expect(page.locator(".order-row")).toHaveCount(1);
    await page.getByRole("menuitem", { name: "捐赠记录", exact: true }).click();
    await expect(page.locator(".donation-history-row")).toHaveCount(1);
    await page.goto(`${base}/donate`);
    await expect(
      page
        .locator(".topbar")
        .getByRole("button", { name: "个人中心", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "退出登录", exact: true }),
    ).toHaveCount(0);

    const remote = await browser.newContext({ locale: "zh-CN" });
    const importer = await remote.newPage();
    await importer.goto(`${base}/center/donations`);
    await expect(importer.locator(".donation-history-row")).toHaveCount(0);
    const input = importer.getByLabel("导入捐赠查询链接", { exact: true });
    await input.fill(receipt.replace(token, "x".repeat(43)));
    await importer.getByRole("button", { name: "导入", exact: true }).click();
    await expect(importer.getByRole("alert")).toContainText(
      "赞赏记录或凭证无效",
    );
    await expect(importer.locator(".donation-history-row")).toHaveCount(0);
    await input.fill(receipt.replace(base, "https://example.invalid"));
    await importer.getByRole("button", { name: "导入", exact: true }).click();
    await expect(importer.getByRole("alert")).toContainText("请输入本站完整");
    await input.fill(receipt);
    await importer.getByRole("button", { name: "导入", exact: true }).click();
    await expect(importer).toHaveURL(`${base}/center/donations/${donation.id}`);
    await expect(
      importer.getByRole("heading", { name: "感谢你的赞赏", exact: true }),
    ).toBeVisible();
    await expect(importer.locator(".topbar")).toHaveCount(1);
    await importer.goto(`${base}/center`);
    await expect(importer.locator(".order-row")).toHaveCount(0);
    await remote.close();

    const detailTab = await context.newPage();
    await detailTab.goto(`${base}/center/donations/${donation.id}`);
    await expect(detailTab.locator("#donation-receipt-link")).toBeVisible();
    await page.goto(`${base}/center/donations`);
    await expect(
      page.getByRole("button", { name: "清除本地捐赠记录", exact: true }),
    ).toBeEnabled();
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      const box = await page.locator(".personal-donations").boundingBox();
      expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
      await page.screenshot({
        path: `../.local/personal-donations-${width}.png`,
        fullPage: true,
      });
    }
    let releaseResponse!: () => void;
    let requestStarted!: () => void;
    const held = new Promise<void>((resolve) => {
      releaseResponse = resolve;
    });
    const started = new Promise<void>((resolve) => {
      requestStarted = resolve;
    });
    await detailTab.route(`**/api/donations/${donation.id}`, async (route) => {
      const response = await route.fetch();
      requestStarted();
      await held;
      await route.fulfill({ response });
    });
    await detailTab.getByRole("button", { name: "刷新", exact: true }).click();
    await started;
    await page
      .getByRole("button", { name: "清除本地捐赠记录", exact: true })
      .click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "清除", exact: true })
      .click();
    releaseResponse();
    await expect(page.locator(".donation-history-row")).toHaveCount(0);
    await expect(detailTab).toHaveURL(`${base}/center/donations`);
    await expect(detailTab.locator("#donation-receipt-link")).toHaveCount(0);
    await page.reload();
    await expect(page.locator(".donation-history-row")).toHaveCount(0);
    await page.goto(`${base}/center/history`);
    await expect(page.locator(".order-row")).toHaveCount(1);
    expect(
      (
        await request.get(`${base}/api/donations/${donation.id}`, {
          headers: { Authorization: `Bearer ${token}` },
        })
      ).ok(),
    ).toBeTruthy();
    await detailTab.close();
  } finally {
    expect(
      (
        await request.put(`${base}/api/admin/donation`, {
          headers,
          data: original,
        })
      ).ok(),
    ).toBeTruthy();
  }
});
