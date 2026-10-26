import { claimContents } from "./fixture";
import { test, expect, loginAdmin } from "./fixture";
import { placeTestOrder } from "./checkout";
import type { Product } from "../src/api";

test("查询中心保留浏览器订单，跨设备导入并同步清除", async ({
  page,
  context,
  browser,
  request,
}) => {
  const base = process.env.HASHDROP_TEST_URL!;
  const adminRequests: string[] = [];
  page.on("request", (req) => {
    if (/\/api\/admin\/(?!session)/.test(req.url()))
      adminRequests.push(req.url());
  });
  const products: Product[] = await (
    await request.get(`${base}/api/products`)
  ).json();
  const product = products.find((item) => item.stock_unlimited)!;
  const orders = [];
  for (let i = 0; i < 2; i++) {
    await page.goto(`${base}/products/${product.id}`);
    await page.getByRole("button", { name: "下单", exact: true }).click();
    const order = await placeTestOrder(page);
    orders.push(order);
    expect(
      (await request.post(`${base}/__test/pay/${order.orderID}`)).ok(),
    ).toBeTruthy();
  }
  await page.goto(`${base}/center`);
  await expect(
    page.getByRole("heading", { name: "订单历史", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".order-row")).toHaveCount(2);
  await expect(page.locator(".workspace-release")).toContainText("HashDrop v");
  await expect(
    page.getByRole("menuitem", { name: "商品", exact: true }),
  ).toHaveCount(0);
  await page.reload();
  await expect(page.locator(".order-row")).toHaveCount(2);
  await expect(
    page.getByRole("menuitem", { name: "查询交付", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "查询订单", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(`${base}/center`);
  await expect(page.locator(".order-row")).toHaveCount(2);
  await expect(page.locator("#claim-key")).toHaveValue("");
  const otherTab = await context.newPage();
  await otherTab.goto(`${base}/center/orders/${orders[0].orderID}`);
  await claimContents(otherTab);
  await expect(
    otherTab.locator(".delivery-item .markdown-content"),
  ).toContainText("购买时内容");
  expect(otherTab.url()).not.toContain(orders[0].key);
  const stored = await page.evaluate(() =>
    Object.entries(localStorage).filter(([key]) =>
      key.startsWith("hashdrop:history:"),
    ),
  );
  expect(stored).toHaveLength(2);
  expect(JSON.stringify(stored)).not.toContain("购买时内容");
  expect(adminRequests).toEqual([]);
  // 登录只追加管理菜单，个人记录不能被全站订单列表替换。
  await page.goto(`${base}/center`);
  await page.getByRole("button", { name: "管理员登录", exact: true }).click();
  await page.locator("#password input").fill("test-password-only");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator(".order-row")).toHaveCount(2);
  await expect(
    page.getByRole("menuitem", { name: "商品", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("menuitem", { name: "订单历史", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("menuitem", { name: "捐赠记录", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await expect(page.locator(".order-row")).toHaveCount(2);
  const pendingTab = await context.newPage();
  await pendingTab.goto(`${base}/products/${product.id}`);
  await pendingTab.evaluate(
    ({ id, productID, key }) => {
      sessionStorage.setItem(
        "hashdrop:checkout",
        JSON.stringify({ requestID: id, productID, key, quantity: 1 }),
      );
    },
    { id: orders[0].orderID, productID: product.id, key: orders[0].key },
  );
  await pendingTab.getByRole("button", { name: "下单", exact: true }).click();
  await expect(pendingTab.locator("#purchase-key")).toHaveValue(orders[0].key);

  const remote = await browser.newContext({ locale: "zh-CN" });
  const importer = await remote.newPage();
  await importer.goto(`${base}/center`);
  await expect(importer.locator(".order-row")).toHaveCount(0);
  await importer.getByRole("button", { name: "查询", exact: true }).click();
  await importer.locator("#claim-key").fill(orders[0].key);
  await importer
    .getByRole("dialog", { name: "查询订单", exact: true })
    .getByRole("button", { name: "查询", exact: true })
    .click();
  await claimContents(importer);
  await expect(
    importer.locator(".delivery-item .markdown-content"),
  ).toContainText("购买时内容");
  await importer.goto(`${base}/center`);
  await expect(importer.locator(".order-row")).toHaveCount(1);
  await remote.close();

  await page.goto(`${base}/center`);
  for (const width of [1440, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    const bounds = await page.locator(".orders-workbench").boundingBox();
    expect(bounds!.x).toBeGreaterThanOrEqual(0);
    expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(width + 1);
    await page.screenshot({
      path: `../.local/center-history-${width}.png`,
      fullPage: true,
    });
  }
  await page
    .getByRole("button", { name: "清除本地订单历史", exact: true })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "清除", exact: true })
    .click();
  await expect(page.locator(".order-row")).toHaveCount(0);
  await expect(otherTab.locator(".delivery-item")).toHaveCount(0);
  await expect(otherTab).toHaveURL(`${base}/center/history`);
  await otherTab.getByRole("button", { name: "查询", exact: true }).click();
  await expect(otherTab.locator("#claim-key")).toHaveValue("");
  await expect(pendingTab.getByRole("dialog")).toHaveCount(0);
  expect(
    await pendingTab.evaluate(() =>
      sessionStorage.getItem("hashdrop:checkout"),
    ),
  ).toBeNull();
  await page.reload();
  await expect(page.locator(".order-row")).toHaveCount(0);
  expect(
    await page.evaluate(() =>
      Object.keys(localStorage).filter((k) =>
        k.startsWith("hashdrop:history:"),
      ),
    ),
  ).toHaveLength(0);
  expect(
    (
      await request.post(`${base}/api/orders/lookup`, {
        data: { key: orders[0].key },
      })
    ).ok(),
  ).toBeTruthy();
  await otherTab.close();
  await pendingTab.close();
});

test("同一框架按会话展示权限并允许重试会话错误", async ({ page }) => {
  const base = process.env.HASHDROP_TEST_URL!;
  await page.goto(`${base}/center/history`);
  await expect(page).toHaveURL(`${base}/center/history`);
  await expect(
    page.getByRole("heading", { name: "订单历史", exact: true }),
  ).toBeVisible();
  await page.goto(`${base}/center/products`);
  await expect(page.getByRole("dialog", { name: "登录后台" })).toBeVisible();
  expect((await page.request.get(`${base}/api/admin/orders`)).status()).toBe(
    401,
  );
  await page.keyboard.press("Escape");
  await loginAdmin(page);
  await page.goto(`${base}/center/products`);
  await expect(page).toHaveURL(`${base}/center/products`);
  await expect(
    page.getByRole("heading", { name: "商品", exact: true }),
  ).toBeVisible();
  const session = await (
    await page.request.get(`${base}/api/admin/session`)
  ).json();
  expect(
    (
      await page.request.post(`${base}/api/admin/logout`, {
        headers: { Origin: base, "X-CSRF-Token": session.csrf },
      })
    ).ok(),
  ).toBeTruthy();
  await page.reload();
  await expect(page).toHaveURL(`${base}/center?login=1`);
  await expect(
    page.getByRole("menuitem", { name: "商品", exact: true }),
  ).toHaveCount(0);
  await page.route("**/api/admin/session", (route) => route.abort());
  await page.goto(`${base}/center`);
  await expect(page.getByRole("alert")).toContainText("无法确认");
  await expect(
    page.getByRole("heading", { name: "订单历史", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(page.locator("#claim-key")).toBeVisible();
  await expect(page.getByRole("alert")).toContainText("无法确认");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  await page.goto(`${base}/center/donations`);
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await expect(page.locator("#donation-query-nickname")).toBeVisible();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "取消", exact: true })
    .click();
  await page.goto(`${base}/center/products`);
  await expect(page.getByRole("alert")).toContainText("无法确认");
  await expect(
    page.getByRole("heading", { name: "商品", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("menuitem", { name: "商品", exact: true }),
  ).toHaveCount(0);
  await page.goto(`${base}/center`);
  await expect(page.getByRole("alert")).toContainText("无法确认");
  await page.unroute("**/api/admin/session");
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "订单历史", exact: true }),
  ).toBeVisible();
});

test("后台切页先显示内容，刷新保留记录且过期会话可重新登录", async ({
  page,
}) => {
  const base = process.env.HASHDROP_TEST_URL!;
  await loginAdmin(page);
  const documentOrigin = await page.evaluate(() => performance.timeOrigin);
  const products: Product[] = await (
    await page.request.get(`${base}/api/admin/products`)
  ).json();
  expect(products.length).toBeGreaterThan(0);
  let sessionRequests = 0;
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/api/admin/session")
      sessionRequests++;
  });
  await page.route("**/api/admin/session", (route) => route.abort());

  let releaseProducts!: () => void;
  const productsGate = new Promise<void>((resolve) => {
    releaseProducts = resolve;
  });
  await page.route("**/api/admin/products", async (route) => {
    await productsGate;
    await route.continue();
  });
  try {
    await page.getByRole("menuitem", { name: "商品", exact: true }).click();
    // 数据响应仍挂起时页面已经可见，不应等认证或出现“没有商品”。
    await expect(
      page.getByRole("heading", { name: "商品", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("columnheader", { name: "商品", exact: true }),
    ).toBeVisible();
    await expect(page.getByText("还没有商品", { exact: true })).toHaveCount(0);
  } finally {
    releaseProducts();
  }
  const productRow = page
    .getByRole("row")
    .filter({ hasText: products[0]!.name })
    .first();
  await expect(productRow).toBeVisible();
  await page.unroute("**/api/admin/products");

  let releaseRefresh!: () => void;
  const refreshGate = new Promise<void>((resolve) => {
    releaseRefresh = resolve;
  });
  await page.route("**/api/admin/products", async (route) => {
    await refreshGate;
    await route.continue();
  });
  try {
    await page.getByRole("button", { name: "刷新", exact: true }).click();
    await expect(productRow).toBeVisible();
    await expect(
      page.getByRole("button", { name: "新增商品", exact: true }),
    ).toBeDisabled();
  } finally {
    releaseRefresh();
  }
  await expect(
    page.getByRole("button", { name: "新增商品", exact: true }),
  ).toBeEnabled();
  await page.unroute("**/api/admin/products");

  let releaseInventories!: () => void;
  const inventoriesGate = new Promise<void>((resolve) => {
    releaseInventories = resolve;
  });
  await page.route("**/api/admin/inventories", async (route) => {
    await inventoriesGate;
    await route.continue();
  });
  try {
    await page.getByRole("menuitem", { name: "库存", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "库存", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: "新增库存池", exact: true }),
    ).toBeDisabled();
  } finally {
    releaseInventories();
  }
  await expect(
    page.getByRole("button", { name: "新增库存池", exact: true }),
  ).toBeEnabled();
  await page.unroute("**/api/admin/inventories");
  await page.getByRole("menuitem", { name: "商品", exact: true }).click();
  await expect(productRow).toBeVisible();
  expect(sessionRequests).toBe(0);
  expect(await page.evaluate(() => performance.timeOrigin)).toBe(
    documentOrigin,
  );

  // 覆盖代理返回非 JSON 的 401：不能留下已登录状态或继续展示管理数据。
  await page.route("**/api/admin/products", (route) =>
    route.fulfill({
      status: 401,
      contentType: "text/plain",
      body: "Session expired",
    }),
  );
  await page.getByRole("button", { name: "刷新", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "登录后台", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("menuitem", { name: "商品", exact: true }),
  ).toHaveCount(0);
  await page.unroute("**/api/admin/products");
  await page.locator("#password input").fill("test-password-only");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByRole("menuitem", { name: "商品", exact: true }),
  ).toBeVisible();

  await page.getByRole("menuitem", { name: "商品", exact: true }).click();
  await expect(productRow).toBeVisible();
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await expect(
    page.getByRole("menuitem", { name: "商品", exact: true }),
  ).toHaveCount(0);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(sessionRequests).toBe(0);
});
