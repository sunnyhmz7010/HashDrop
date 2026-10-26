import { test, expect, loginAdmin, type Page } from "./fixture";
import { placeTestOrder } from "./checkout";
import type { Donation, Product } from "../src/api";

const base = () => process.env.HASHDROP_TEST_URL!;
const nickname = (label: string) =>
  `${label}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

async function createDonation(page: Page, name: string) {
  await page.goto(`${base()}/donate`);
  await page.locator("#donate-nickname").fill(name);
  const created = page.waitForResponse(
    (response) =>
      response.url() === `${base()}/api/donations` &&
      response.request().method() === "POST",
  );
  await page
    .getByRole("button", { name: "确认金额并赞赏", exact: true })
    .click();
  const response = await created;
  expect(response.ok(), await response.text()).toBeTruthy();
  const donation = ((await response.json()) as { donation: Donation }).donation;
  await expect(page).toHaveURL(`${base()}/center/donations/${donation.id}`);
  await expect(
    page.getByRole("button", { name: "前往 HashPay 付款", exact: true }),
  ).toBeEnabled({ timeout: 30_000 });
  return donation;
}

test("个人捐赠记录按昵称查询和清除，管理员及会话故障时仍可访问", async ({
  page,
  request,
  browser,
  context,
}) => {
  test.setTimeout(90_000);
  const login = await request.post(`${base()}/api/admin/login`, {
    headers: { Origin: base() },
    data: { username: "admin", password: "test-password-only" },
  });
  expect(login.ok()).toBeTruthy();
  const headers = { Origin: base(), "X-CSRF-Token": (await login.json()).csrf };
  const original = await (
    await request.get(`${base()}/api/admin/donation`)
  ).json();
  expect(
    (
      await request.put(`${base()}/api/admin/donation`, {
        headers,
        data: { ...original, enabled: true },
      })
    ).ok(),
  ).toBeTruthy();
  try {
    const products: Product[] = await (
      await request.get(`${base()}/api/products`)
    ).json();
    const product = products.find((item) => item.stock_unlimited)!;
    await page.goto(`${base()}/products/${product.id}`);
    await expect(
      page
        .locator(".topbar")
        .getByRole("button", { name: "个人中心", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "下单", exact: true }).click();
    await placeTestOrder(page);
    const name = nickname("个人赞赏昵称");
    const donation = await createDonation(page, name);
    await expect(page.locator("#donation-receipt-link")).toHaveCount(0);
    expect(
      (await request.post(`${base()}/__test/pay-donation/${donation.id}`)).ok(),
    ).toBeTruthy();
    await page.goto(`${base()}/center/donations`);
    await expect(page.locator(".donation-history-row")).toHaveCount(1);
    await page.reload();
    await expect(page.locator(".donation-history-row")).toContainText(
      donation.id,
    );
    await expect(page.locator(".donation-history-row")).toContainText(name);
    await expect(page.locator(".donation-history-row")).toContainText("已付款");
    await expect(page.locator("#import-donation-link")).toHaveCount(0);
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
    await page.goto(`${base()}/donate`);
    await expect(
      page
        .locator(".topbar")
        .getByRole("button", { name: "个人中心", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "退出登录", exact: true }),
    ).toHaveCount(0);

    const remote = await browser.newContext({ locale: "zh-CN" });
    const querier = await remote.newPage();
    try {
      // 管理会话请求失败不应阻断访客的真实昵称查询接口。
      await querier.route(`${base()}/api/admin/session`, (route) =>
        route.fulfill({
          status: 503,
          contentType: "application/json",
          body: JSON.stringify({ error: "管理员会话暂时不可用" }),
        }),
      );
      await querier.goto(`${base()}/center/donations`);
      await expect(querier.locator(".donation-history-row")).toHaveCount(0);
      await expect(querier.locator("#donation-query-nickname")).toHaveCount(0);
      await querier.getByRole("button", { name: "查询", exact: true }).click();
      const query = querier.getByRole("dialog", {
        name: "查询捐赠",
        exact: true,
      });
      await expect(query).toBeVisible();
      await expect(querier).toHaveURL(`${base()}/center/donations`);
      const input = querier.locator("#donation-query-nickname");
      await expect(input).toBeVisible();
      await input.fill(nickname("不存在的昵称"));
      const missing = querier.waitForResponse(
        (response) =>
          response.url() === `${base()}/api/donations/lookup` &&
          response.request().method() === "POST",
      );
      await query.getByRole("button", { name: "查询", exact: true }).click();
      expect((await missing).status()).toBe(404);
      await expect(
        querier.getByRole("alert").filter({ hasText: /赞赏|捐赠/ }),
      ).toBeVisible();
      await expect(querier.locator(".donation-history-row")).toHaveCount(0);
      await input.fill(name);
      await query.getByRole("button", { name: "查询", exact: true }).click();
      await expect(querier).toHaveURL(
        `${base()}/center/donations/${donation.id}`,
      );
      await expect(
        querier.getByRole("heading", { name: "感谢你的赞赏", exact: true }),
      ).toBeVisible();
      await expect(querier.locator(".donation-receipt")).toContainText(name);
      await expect(querier.locator("#donation-receipt-link")).toHaveCount(0);
      await querier.goto(`${base()}/center/donations`);
      await expect(querier.locator(".donation-history-row")).toContainText(
        name,
      );
      await querier.goto(`${base()}/center`);
      await expect(querier.locator(".order-row")).toHaveCount(0);
    } finally {
      await remote.close();
    }

    const detailTab = await context.newPage();
    await detailTab.goto(`${base()}/center/donations/${donation.id}`);
    await expect(detailTab.locator(".donation-receipt")).toContainText(name);
    await page.goto(`${base()}/center/donations`);
    await expect(
      page.getByRole("button", { name: "清除本地捐赠记录", exact: true }),
    ).toBeEnabled();
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      const box = await page.locator(".personal-donations").boundingBox();
      expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
      await page.screenshot({
        path: `../.local/donation-history-${width}.png`,
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
    await detailTab.route(
      `${base()}/api/donations/${donation.id}`,
      async (route) => {
        const response = await route.fetch();
        requestStarted();
        await held;
        await route.fulfill({ response });
      },
    );
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
    await expect(detailTab).toHaveURL(`${base()}/center/donations`);
    await expect(detailTab.locator(".donation-receipt")).toHaveCount(0);
    await page.reload();
    await expect(page.locator(".donation-history-row")).toHaveCount(0);
    await page.goto(`${base()}/center/history`);
    await expect(page.locator(".order-row")).toHaveCount(1);
    expect(
      (
        await request.post(`${base()}/api/donations/lookup`, {
          data: { nickname: name },
        })
      ).ok(),
    ).toBeTruthy();
    await detailTab.close();
  } finally {
    expect(
      (
        await request.put(`${base()}/api/admin/donation`, {
          headers,
          data: original,
        })
      ).ok(),
    ).toBeTruthy();
  }
});

test("一条本地赞赏失效后仍刷新后续已付款记录", async ({ page, request }) => {
  const login = await request.post(`${base()}/api/admin/login`, {
    headers: { Origin: base() },
    data: { username: "admin", password: "test-password-only" },
  });
  expect(login.ok()).toBeTruthy();
  const headers = { Origin: base(), "X-CSRF-Token": (await login.json()).csrf };
  const original = await (
    await request.get(`${base()}/api/admin/donation`)
  ).json();
  expect(
    (
      await request.put(`${base()}/api/admin/donation`, {
        headers,
        data: { ...original, enabled: true },
      })
    ).ok(),
  ).toBeTruthy();
  try {
    await createDonation(page, nickname("较早的赞赏"));
    await createDonation(page, nickname("较新的赞赏"));
    await page.goto(`${base()}/center/donations`);
    const rows = page.locator(".donation-history-row");
    await expect(rows).toHaveCount(2);
    await expect(rows.nth(0)).toContainText("等待付款");
    await expect(rows.nth(1)).toContainText("等待付款");
    const firstID = (
      await rows.nth(0).getByRole("button").first().innerText()
    ).trim();
    const secondID = (
      await rows.nth(1).getByRole("button").first().innerText()
    ).trim();
    await page.route(`${base()}/api/donations/${firstID}`, (route) =>
      route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: "赞赏记录或凭证无效" }),
      }),
    );
    expect(
      (await request.post(`${base()}/__test/pay-donation/${secondID}`)).ok(),
    ).toBeTruthy();
    const updated = page.waitForResponse(
      (response) =>
        response.url() === `${base()}/api/donations/${secondID}` &&
        response.request().method() === "GET",
    );
    await page.getByRole("button", { name: "刷新", exact: true }).click();
    expect((await updated).ok()).toBeTruthy();
    await expect(rows.filter({ hasText: secondID })).toContainText("已付款");
    await expect(rows.filter({ hasText: firstID })).toBeVisible();
    await expect(page.getByRole("alert")).toContainText("赞赏记录或凭证无效");
  } finally {
    expect(
      (
        await request.put(`${base()}/api/admin/donation`, {
          headers,
          data: original,
        })
      ).ok(),
    ).toBeTruthy();
  }
});
