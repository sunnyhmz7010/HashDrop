import { randomBytes } from "node:crypto";
import { resolve } from "node:path";
import { test, expect, loginAdmin, createInventory } from "./fixture";

test("订单金额筛选、数字分页与失败查询重试", async ({ page }) => {
  const base = process.env.HASHDROP_TEST_URL!;
  await loginAdmin(page);
  const pool = await createInventory(
    page,
    "分页验证库存",
    "static",
    "分页验证内容",
  );
  const session = await (
    await page.request.get(`${base}/api/admin/session`)
  ).json();
  const headers = { Origin: base, "X-CSRF-Token": session.csrf };
  const response = await page.request.post(`${base}/api/admin/products`, {
    headers,
    data: {
      name: "分页验证商品",
      price: 987,
      currency: "JPY",
      inventory_id: pool.id,
      active: true,
      hidden: true,
    },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  const product = await response.json();
  for (let i = 0; i < 161; i++) {
    if (i % 8 === 0) await page.request.post(`${base}/__test/reset-limits`);
    const created = await page.request.post(`${base}/api/orders`, {
      data: {
        product_id: product.id,
        quantity: 1,
        key: `filter-test-key-${i}`,
        request_id: randomBytes(16).toString("hex"),
        quoted_total: 987,
        quoted_currency: "JPY",
      },
    });
    expect(created.status(), await created.text()).toBe(201);
  }
  await page.goto(`${base}/center/orders`);
  await page.getByLabel("订单币种", { exact: true }).click();
  await page
    .locator(".n-base-select-option")
    .getByText("JPY", { exact: true })
    .click();
  await page.getByLabel("最低订单金额", { exact: true }).fill("987");
  await page.getByLabel("最高订单金额", { exact: true }).fill("987");
  await page.getByRole("button", { name: "筛选", exact: true }).click();
  await expect(page.getByText("共 161 笔订单", { exact: true })).toBeVisible();
  await expect(page.locator(".orders-table .order-row")).toHaveCount(20);
  await expect(
    page.locator(".n-pagination-item").filter({ hasText: /^9$/ }),
  ).toBeVisible();
  await page
    .locator(".orders-pagination")
    .screenshot({ path: resolve("../.local/orders-pagination.png") });
  await page.screenshot({
    path: resolve("../.local/orders-filters-1440.png"),
    fullPage: true,
  });
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await page
      .getByRole("button", { name: "筛选", exact: true })
      .scrollIntoViewIfNeeded();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBeFalsy();
    const filterBounds = await page.locator(".orders-filter").boundingBox();
    expect(filterBounds!.x + filterBounds!.width).toBeLessThanOrEqual(width);
    await page.screenshot({
      path: resolve(`../.local/orders-filters-${width}.png`),
    });
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  const firstPage = await page.locator(".order-id-button").allTextContents();
  await page.locator(".n-pagination-item").filter({ hasText: /^2$/ }).click();
  await expect
    .poll(async () => page.locator(".order-id-button").first().innerText())
    .not.toBe(firstPage[0]);
  expect(
    (await page.locator(".order-id-button").allTextContents()).some((id) =>
      firstPage.includes(id),
    ),
  ).toBe(false);
  await page.getByLabel("最低订单金额", { exact: true }).fill("988");
  await page.getByLabel("最高订单金额", { exact: true }).fill("988");
  await page.route("**/api/admin/orders?*", (route) => route.abort(), {
    times: 1,
  });
  await page.getByRole("button", { name: "筛选", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "重试", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect(page.getByText("共 0 笔订单", { exact: true })).toBeVisible();
  await expect(
    page.getByText("没有符合条件的订单", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "重置", exact: true }).click();
  await expect(page.getByLabel("最低订单金额", { exact: true })).toHaveValue(
    "",
  );
  await expect(page.getByText("共 161 笔订单", { exact: true })).toBeVisible();

  // 已提交的高级筛选失败后继续搜索，不能悄悄退回上次成功的筛选范围。
  await page.getByLabel("订单币种", { exact: true }).click();
  await page
    .locator(".n-base-select-option")
    .getByText("JPY", { exact: true })
    .click();
  await page.getByLabel("最低订单金额", { exact: true }).fill("988");
  await page.getByLabel("最高订单金额", { exact: true }).fill("988");
  await page.route("**/api/admin/orders?*", (route) => route.abort(), {
    times: 1,
  });
  await page.getByRole("button", { name: "筛选", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "重试", exact: true }),
  ).toBeVisible();
  const searched = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      url.pathname === "/api/admin/orders" &&
      url.searchParams.get("q") === firstPage[0]!.trim()
    );
  });
  await page.locator("#orders-search").fill(firstPage[0]!.trim());
  await searched;
  await expect(page.getByText("共 0 笔订单", { exact: true })).toBeVisible();
  await expect(
    page.getByText("没有符合条件的订单", { exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("最低订单金额", { exact: true })).toHaveValue(
    "988",
  );
});

test("商城公告复用 Markdown 编辑预览并展示图片视频", async ({ page }) => {
  const base = process.env.HASHDROP_TEST_URL!;
  await loginAdmin(page);
  await page.goto(`${base}/center/settings`);
  const notice = `## 公告验证\n\n**重要公告**\n\n![公告图片](${base}/__test/media/image.svg)\n\n![公告视频](${base}/__test/media/clip.webm)\n\n<script>alert(1)</script>`;
  await page.getByLabel("商城公告（Markdown）", { exact: true }).fill(notice);
  await page.locator(".n-tabs-tab").getByText("预览", { exact: true }).click();
  await expect(page.locator(".markdown-content img")).toBeVisible();
  await expect(page.locator(".markdown-content video")).toHaveAttribute(
    "controls",
    "",
  );
  await page.getByRole("button", { name: "保存设置", exact: true }).click();
  await expect(page.getByText("设置已保存", { exact: true })).toBeVisible();
  await page.reload();
  await expect(
    page.getByLabel("商城公告（Markdown）", { exact: true }),
  ).toHaveValue(notice);
  await page.goto(base);
  const announcement = page.getByRole("region", {
    name: "商城公告（Markdown）",
    exact: true,
  });
  await expect(
    announcement.getByRole("heading", { name: "公告验证", exact: true }),
  ).toBeVisible();
  await expect(announcement.locator("strong")).toHaveText("重要公告");
  await expect(announcement.locator("img")).toBeVisible();
  await expect(announcement.locator("video")).toHaveAttribute("controls", "");
  await expect(announcement.locator("script")).toHaveCount(0);
  await page.setViewportSize({ width: 390, height: 900 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth + 1,
    ),
  ).toBeFalsy();
  await page.screenshot({
    path: resolve("../.local/markdown-announcement-390.png"),
    fullPage: true,
  });
  // 还原公共公告，避免其他场景额外加载媒体。
  await page.goto(`${base}/center/settings`);
  await page.getByLabel("商城公告（Markdown）", { exact: true }).fill("");
  await page.getByRole("button", { name: "保存设置", exact: true }).click();
  await expect(page.getByText("设置已保存", { exact: true })).toBeVisible();
});
