import {
  test,
  expect,
  createInventory,
  bindInventoryProducts,
  importInventory,
} from "./fixture";
import type { Product } from "../src/api";
import { placeTestOrder } from "./checkout";

test("匿名购买、凭证领取与管理员库存管理", async ({ page, request }) => {
  const base = process.env.HASHDROP_TEST_URL!;
  await page.goto(base);
  await expect(page.locator(".product-card").first()).toBeVisible();
  const products: Product[] = await (
    await request.get(`${base}/api/products`)
  ).json();
  const cardProduct = products.find((product) => product.kind === "card")!;
  const card = page
    .locator(".product-card")
    .filter({ has: page.locator(`a[href="/products/${cardProduct.id}"]`) });
  await card.getByRole("link", { name: "详情" }).click();
  await expect(page.locator(".product-summary")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "下单", exact: true }),
  ).toBeEnabled();
  await page.setViewportSize({ width: 390, height: 900 });
  const productID = new URL(page.url()).pathname.split("/").pop()!;
  const productData = await (
    await request.get(`${base}/api/products/${productID}`)
  ).json();
  await page.route(`**/api/products/${productID}`, (route) =>
    route.fulfill({
      json: {
        ...productData,
        name: "LongProductName".repeat(8),
        description: "LongDescription".repeat(20),
      },
    }),
  );
  await page.reload();
  await expect(
    page.getByRole("button", { name: "下单", exact: true }),
  ).toBeEnabled();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth + 1,
    ),
  ).toBeFalsy();
  await expect(page.getByRole("link", { name: "返回商城" })).toBeInViewport();
  await page.unroute(`**/api/products/${productID}`);
  await page.reload();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByRole("button", { name: "下单", exact: true }).click();
  const { key: token, orderID, receipt } = await placeTestOrder(page);
  await page.goto(receipt);
  await expect(page.getByRole("heading", { name: "订单状态" })).toBeVisible();
  // 公网普通 HTTP 没有 Clipboard API，使用浏览器复制后仍能保存领取密钥。
  await page.evaluate(() =>
    Object.defineProperty(window, "isSecureContext", { value: false }),
  );
  await page.getByRole("button", { name: "复制密钥", exact: true }).click();
  await expect(page.getByText("已复制", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "前往 HashPay 付款" }),
  ).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText("已复制", { exact: true })).toBeHidden();
  await page.setViewportSize({ width: 390, height: 900 });
  await expect(
    page.getByRole("button", { name: "前往 HashPay 付款" }),
  ).toBeInViewport();
  await expect(page.locator(".checkout-copy-field code")).toBeInViewport();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth + 1,
    ),
  ).toBeFalsy();
  await page.setViewportSize({ width: 1440, height: 1000 });
  const detail = await request.get(`${base}/api/orders/${orderID}`, {
    headers: {
      Authorization: `ClaimKey ${Buffer.from(token).toString("base64url")}`,
    },
  });
  const initialDetail = await detail.json();
  const gateway = initialDetail.order.checkout_url as string;
  expect(gateway).not.toContain(token);
  expect(gateway).not.toContain("#token=");
  // 仅替换读取响应检查展示边界；实际付款仍经过下方真实签名网关流程。
  for (const paymentStatus of ["expired", "invalid", "closed"]) {
    await page.route(`**/api/orders/${orderID}`, (route) =>
      route.fulfill({
        json: {
          ...initialDetail,
          order: { ...initialDetail.order, payment_status: paymentStatus },
        },
      }),
    );
    await page.reload();
    await expect(page.locator(".checkout-expired-state")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "前往 HashPay 付款" }),
    ).toHaveCount(0);
    await expect(page.locator(".checkout-copy-field code")).toHaveText(token);
    await page.unroute(`**/api/orders/${orderID}`);
  }
  expect(
    (await request.post(`${base}/__test/pay/${orderID}`)).ok(),
  ).toBeTruthy();
  await page.reload();
  await expect(page.locator(".delivery-item pre")).toContainText("卡密-");
  await page.goto(`${base}/center/orders/${orderID}`);
  await page.getByRole("button", { name: "查询其他订单", exact: true }).click();
  await page.locator("#claim-key").fill(token);
  await page
    .getByRole("dialog", { name: "查询订单", exact: true })
    .getByRole("button", { name: "查询", exact: true })
    .click();
  await expect(page).toHaveURL(`${base}/center/orders/${orderID}`);
  await expect(page.locator(".delivery-item pre")).toContainText("卡密-");
  await page.goto(`${base}/center?login=1`);
  await page.locator("#password input").fill("test-password-only");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "退出登录", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "订单历史", exact: true }),
  ).toBeVisible();
  const pool = await createInventory(page, "浏览器验证库存");
  await page.getByRole("menuitem", { name: "商品", exact: true }).click();
  await page.getByRole("button", { name: "新增商品" }).click();
  await page.locator("#product-name input").fill("浏览器验证商品");
  await page.locator("#price input").fill("18.50");
  const saved = page.waitForResponse(
    (response) =>
      response.url() === `${base}/api/admin/products` &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "保存商品" }).click();
  const createdProduct = await (await saved).json();
  await bindInventoryProducts(page, pool, [createdProduct.id]);
  const row = page.getByRole("row").filter({ hasText: "浏览器验证商品" });
  await expect(row).toContainText("18.50 CNY");
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(page.locator("#product-name input")).toHaveValue(
    "浏览器验证商品",
  );
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await importInventory(page, pool, "E2E-001\nE2E-002\nE2E-001");
  const inventoryRow = page.getByRole("row").filter({ hasText: pool.id });
  await expect(inventoryRow).toContainText("2 / 0 / 0");
  await page.route("**/api/admin/inventories/*/stock", (route) =>
    route.fulfill({ status: 503, json: { error: "库存查询暂时失败" } }),
  );
  await inventoryRow.getByRole("button", { name: "库存", exact: true }).click();
  const inventoryDialog = page.getByRole("dialog", {
    name: "库存 · 浏览器验证库存",
    exact: true,
  });
  await expect(inventoryDialog.getByText("库存查询暂时失败")).toBeVisible();
  await expect(inventoryDialog.getByText("暂无库存")).toHaveCount(0);
  await page.unroute("**/api/admin/inventories/*/stock");
  await inventoryDialog.getByRole("button", { name: "重新查询" }).click();
  await expect(
    inventoryDialog.getByRole("cell", { name: "可用", exact: true }),
  ).toHaveCount(2);
  await page.keyboard.press("Escape");
  await page.getByRole("menuitem", { name: "订单", exact: true }).click();
  await expect(
    page.locator(".order-row").filter({ hasText: orderID }),
  ).toContainText("交付完成");
  // 只读详情允许取消；关闭时必须中止旧请求，重新打开只提交当前请求的结果。
  await page.route(`**/api/admin/orders/${orderID}`, async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 600));
    if (!route.request().failure()) await route.continue();
  });
  const cancelledDetail = page.waitForEvent(
    "requestfailed",
    (request) =>
      new URL(request.url()).pathname === `/api/admin/orders/${orderID}`,
  );
  await page
    .locator(".order-row")
    .filter({ hasText: orderID })
    .getByRole("button", { name: orderID, exact: true })
    .click();
  await page.keyboard.press("Escape");
  await expect(page.locator(".form-modal")).toHaveCount(0);
  await cancelledDetail;
  await page.unroute(`**/api/admin/orders/${orderID}`);
  await page
    .locator(".order-row")
    .filter({ hasText: orderID })
    .getByRole("button", { name: orderID, exact: true })
    .click();
  await expect(
    page
      .getByRole("dialog", { name: "订单详情" })
      .getByText(orderID, { exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".form-modal")).toHaveCount(0);
  await page.getByRole("menuitem", { name: "系统设置", exact: true }).click();
  await expect(page.locator("#site-title input")).toHaveValue("HashDrop");
  await page.locator("#site-title input").fill("HashDrop 测试店铺");
  await page.getByRole("button", { name: "保存设置" }).click();
  await expect(page.getByText("设置已保存")).toBeVisible();
  await page.reload();
  await expect(page.locator("#site-title input")).toHaveValue(
    "HashDrop 测试店铺",
  );
  await expect(page.locator(".n-spin-content--spinning")).toHaveCount(0);
  for (const width of [1440, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(base);
    await expect(page.locator(".product-card").first()).toBeVisible();
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth + 1,
    );
    expect(overflow).toBeFalsy();
    await page.goto(`${base}/center/products`);
    await expect(page.getByRole("button", { name: "新增商品" })).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth + 1,
      ),
    ).toBeFalsy();
    await page.getByRole("button", { name: "新增商品" }).click();
    await expect(page.locator(".form-modal")).toBeVisible();
    expect(
      await page.locator(".form-modal").evaluate((el) => {
        const rect = el.getBoundingClientRect();
        return rect.left >= 0 && rect.right <= window.innerWidth + 1;
      }),
    ).toBeTruthy();
    await page.getByRole("button", { name: "取消", exact: true }).click();
    if (width < 820) {
      await page.getByRole("button", { name: "打开管理菜单" }).click();
      await page.getByRole("menuitem", { name: "订单", exact: true }).click();
      await expect(
        page.getByRole("heading", { name: "订单", exact: true }),
      ).toBeVisible();
      await expect(
        page.getByRole("menuitem", { name: "订单", exact: true }),
      ).toHaveCount(0);
      await page.getByRole("button", { name: "打开管理菜单" }).click();
      await page.getByRole("menuitem", { name: "商品", exact: true }).click();
      await expect(
        page.getByRole("button", { name: "新增商品" }),
      ).toBeVisible();
    }
  }
  // 手机横屏下正文可滚动，编辑标题和保存操作仍留在视口内。
  await page.setViewportSize({ width: 390, height: 450 });
  await page.getByRole("button", { name: "新增商品" }).click();
  const editor = page.getByRole("dialog", { name: "新增商品" });
  await expect(editor).toBeVisible();
  await expect(
    editor.getByRole("button", { name: "保存商品" }),
  ).toBeInViewport();
  await expect(editor.getByText("新增商品", { exact: true })).toBeInViewport();
  expect(
    await editor.evaluate((el) => {
      const rect = el.getBoundingClientRect();
      return rect.top >= 0 && rect.bottom <= innerHeight + 1;
    }),
  ).toBeTruthy();
  await editor.locator("#description textarea").fill("短屏编辑内容");
  await editor.locator("#description textarea").scrollIntoViewIfNeeded();
  await expect(editor.locator("#description textarea")).toBeInViewport();
  await expect(
    editor.getByRole("button", { name: "保存商品" }),
  ).toBeInViewport();
  await expect(editor.getByText("新增商品", { exact: true })).toBeInViewport();
  await editor.getByRole("button", { name: "取消", exact: true }).click();
  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto(receipt);
  await expect(page.locator(".delivery-item pre")).toContainText("卡密-");
  const interrupted = await page.context().newPage();
  await interrupted.route("**/api/setup/status", (route) => route.abort());
  await interrupted.goto(receipt);
  await expect(interrupted.locator(".delivery-item pre")).toContainText(
    "卡密-",
  );
  expect(interrupted.url()).toBe(receipt);
  await interrupted.close();
  await page.goto(`${base}/center/products`);
  await expect(
    page.getByRole("row").filter({ hasText: "浏览器验证商品" }),
  ).toBeVisible();
  await expect(page.locator(".n-spin-content--spinning")).toHaveCount(0);
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(base);
  await expect(page.locator(".product-card").first()).toBeVisible();
  await expect(page.locator(".n-spin-content--spinning")).toHaveCount(0);
  await page.goto(`${base}/center/orders`);
  await expect(page.locator(".order-row").first()).toBeVisible();
  await expect(page.locator(".n-spin-content--spinning")).toHaveCount(0);
  const blocked = await page.context().newPage();
  await blocked.addInitScript(() => {
    Storage.prototype.setItem = () => {
      throw new Error("存储不可用");
    };
  });
  await blocked.goto(base);
  await blocked
    .locator(".product-card")
    .filter({ has: blocked.locator(`a[href="/products/${cardProduct.id}"]`) })
    .getByRole("link", { name: "详情" })
    .click();
  await expect(blocked.locator(".product-summary")).toBeVisible();
  await blocked.getByRole("button", { name: "下单", exact: true }).click();
  await blocked.getByRole("button", { name: "生成", exact: true }).click();
  await blocked.getByRole("button", { name: "前往支付", exact: true }).click();
  await expect(
    blocked.getByRole("button", { name: "前往支付", exact: true }),
  ).toBeDisabled();
  await expect(
    blocked.getByText("我已手动保存领取密钥", { exact: true }),
  ).toBeVisible();
  await expect(blocked).toHaveURL(`${base}/products/${cardProduct.id}`);
  // 复制成功后即可付款，不要求重复确认同一备份动作。
  await blocked.evaluate(() =>
    Object.defineProperty(window, "isSecureContext", { value: false }),
  );
  await blocked.getByRole("button", { name: "复制", exact: true }).click();
  const blockedOrder = await placeTestOrder(blocked);
  await blocked.goto(`${base}/center/orders/${blockedOrder.orderID}`);
  await blocked.getByRole("button", { name: "查询", exact: true }).click();
  await blocked.locator("#claim-key").fill(blockedOrder.key);
  await blocked
    .getByRole("dialog", { name: "查询订单", exact: true })
    .getByRole("button", { name: "查询", exact: true })
    .click();
  await expect(
    blocked.getByRole("button", { name: "前往 HashPay 付款" }),
  ).toBeDisabled();
  await blocked.evaluate(() =>
    Object.defineProperty(window, "isSecureContext", { value: false }),
  );
  await blocked.getByRole("button", { name: "复制密钥", exact: true }).click();
  await expect(
    blocked.getByRole("button", { name: "前往 HashPay 付款" }),
  ).toBeEnabled();
  await blocked.close();
  await page.goto(`${base}/center/orders`);
  const paidRow = page.locator(".order-row").filter({ hasText: orderID });
  await paidRow.getByRole("button", { name: orderID, exact: true }).click();
  await page.getByRole("button", { name: "刷新状态", exact: true }).click();
  await expect(page.getByText("订单状态已更新", { exact: true })).toBeVisible();
  await expect(page.locator(".form-modal .delivery-item pre")).toContainText(
    "卡密-",
  );
  await expect(page.locator(".form-modal")).toBeVisible();
  await expect(page.getByText("订单状态已更新", { exact: true })).toBeHidden();
  await page.setViewportSize({ width: 390, height: 900 });
  await expect(page.getByRole("dialog", { name: "订单详情" })).toBeInViewport();
  const orderDialog = page.getByRole("dialog", { name: "订单详情" });
  await orderDialog.locator(".delivery-item pre").scrollIntoViewIfNeeded();
  await expect(orderDialog.locator(".delivery-item pre")).toBeInViewport();
  await expect(
    orderDialog.getByText("订单详情", { exact: true }),
  ).toBeInViewport();
  await page.setViewportSize({ width: 1440, height: 1000 });
  let finishDelete = () => {};
  const pendingDelete = new Promise<void>((resolve) => {
    finishDelete = resolve;
  });
  await page.route(`**/api/admin/orders/${orderID}`, async (route) => {
    if (route.request().method() === "DELETE") {
      await pendingDelete;
      await route.fulfill({
        status: 503,
        contentType: "application/json",
        body: JSON.stringify({ error: "删除暂时失败" }),
      });
    } else await route.continue();
  });
  await page.getByRole("button", { name: "删除订单", exact: true }).click();
  const confirmation = page.locator(".n-dialog");
  await confirmation
    .getByRole("button", { name: "删除订单", exact: true })
    .click();
  try {
    await expect(
      confirmation.getByRole("button", { name: "取消", exact: true }),
    ).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(confirmation).toBeVisible();
  } finally {
    finishDelete();
  }
  await expect(
    confirmation.getByRole("button", { name: "取消", exact: true }),
  ).toBeEnabled();
  await expect(page.locator(".form-modal")).toBeVisible();
  await expect(paidRow).toBeVisible();
  await expect(
    confirmation.getByRole("button", { name: "删除订单", exact: true }),
  ).toBeEnabled();
  await page.unroute(`**/api/admin/orders/${orderID}`);
  await confirmation
    .getByRole("button", { name: "删除订单", exact: true })
    .click();
  await expect(paidRow).toHaveCount(0);
  await page.reload();
  await expect(paidRow).toHaveCount(0);
  await page.goto(receipt);
  await expect(page.locator(".delivery-item pre")).toContainText("卡密-");
});
