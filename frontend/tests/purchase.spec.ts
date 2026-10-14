import { test, expect } from "@playwright/test";

test("匿名购买、凭证领取与管理员库存管理", async ({ page, request }) => {
  const base = process.env.TINYFAKA_TEST_URL!;
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(base);
  await expect(page.locator(".product-card").first()).toBeVisible();
  const card = page.locator(".product-card").filter({ hasText: "卡密" });
  await card.getByRole("button", { name: "查看商品" }).click();
  await page.getByRole("button", { name: "创建订单", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "先保存领取链接" }),
  ).toBeVisible();
  // 公网普通 HTTP 没有 Clipboard API，使用浏览器复制后仍能保存领取链接。
  await page.evaluate(() =>
    Object.defineProperty(window, "isSecureContext", { value: false }),
  );
  await page.getByRole("button", { name: "复制链接", exact: true }).click();
  await expect(page.getByText("已复制", { exact: true })).toBeVisible();
  const receipt = page.url();
  const url = new URL(receipt),
    token = new URLSearchParams(url.hash.slice(1)).get("token")!,
    orderID = url.pathname.split("/").pop()!;
  expect(token).toHaveLength(43);
  await expect(
    page.getByRole("button", { name: "前往 HashPay 付款" }),
  ).toBeVisible({ timeout: 15_000 });
  const detail = await request.get(`${base}/api/orders/${orderID}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  const gateway = (await detail.json()).order.checkout_url as string;
  expect(gateway).not.toContain(token);
  expect(gateway).not.toContain("#token=");
  expect(
    (await request.post(`${base}/__test/pay/${orderID}`)).ok(),
  ).toBeTruthy();
  await page.reload();
  await expect(page.locator(".delivery-item pre")).toContainText("卡密-");
  await page.goto(`${base}/admin/login`);
  await page.locator("#password input").fill("test-password-only");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "概览", exact: true }),
  ).toBeVisible();
  await page.getByRole("menuitem", { name: "商品与库存", exact: true }).click();
  await page.getByRole("button", { name: "新增商品" }).click();
  await page.locator("#product-name input").fill("浏览器验证商品");
  await page.locator("#price input").fill("18.50");
  await page.getByRole("button", { name: "保存商品" }).click();
  const row = page.getByRole("row").filter({ hasText: "浏览器验证商品" });
  await expect(row).toContainText("18.50 CNY");
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(page.locator("#product-name input")).toHaveValue(
    "浏览器验证商品",
  );
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await row.getByRole("button", { name: "导入", exact: true }).click();
  await page.getByPlaceholder("每行一份库存").fill("E2E-001\nE2E-002\nE2E-001");
  await page.getByRole("button", { name: "导入库存", exact: true }).click();
  await expect(row).toContainText("2 / 0 / 0");
  await page.getByRole("menuitem", { name: "订单", exact: true }).click();
  await expect(
    page.locator(".order-row").filter({ hasText: orderID }),
  ).toContainText("交付完成");
  // 详情加载中不能通过 Esc 丢弃请求并把旧结果提交给另一订单。
  await page.route(`**/api/admin/orders/${orderID}`, async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 600));
    await route.continue();
  });
  await page
    .locator(".order-row")
    .filter({ hasText: orderID })
    .getByRole("button", { name: orderID, exact: true })
    .click();
  await page.keyboard.press("Escape");
  await expect(page.locator(".form-modal")).toBeVisible();
  await expect(page.locator(".form-modal .code-cell")).toContainText(orderID);
  await page.unroute(`**/api/admin/orders/${orderID}`);
  await page.keyboard.press("Escape");
  await expect(page.locator(".form-modal")).toHaveCount(0);
  await page.getByRole("menuitem", { name: "系统设置", exact: true }).click();
  await expect(page.locator("#site-title input")).toHaveValue("TinyFaka");
  await page.locator("#site-title input").fill("TinyFaka 测试店铺");
  await page.getByRole("button", { name: "保存设置" }).click();
  await expect(page.getByText("设置已保存")).toBeVisible();
  await page.reload();
  await expect(page.locator("#site-title input")).toHaveValue(
    "TinyFaka 测试店铺",
  );
  await expect(page.locator(".n-spin-content")).toHaveCSS("opacity", "1");
  await page.screenshot({
    path: "../.local/settings-desktop.png",
    fullPage: true,
  });
  for (const width of [1440, 768, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(base);
    await expect(page.locator(".product-card").first()).toBeVisible();
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth + 1,
    );
    expect(overflow).toBeFalsy();
    await page.goto(`${base}/admin/products`);
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
        page.getByRole("heading", { name: "订单管理" }),
      ).toBeVisible();
      await expect(
        page.getByRole("menuitem", { name: "订单", exact: true }),
      ).toHaveCount(0);
      await page.getByRole("button", { name: "打开管理菜单" }).click();
      await page
        .getByRole("menuitem", { name: "商品与库存", exact: true })
        .click();
      await expect(
        page.getByRole("button", { name: "新增商品" }),
      ).toBeVisible();
    }
  }
  // 手机横屏下正文可滚动，编辑标题和保存操作仍留在视口内。
  await page.setViewportSize({ width: 390, height: 450 });
  await page.getByRole("button", { name: "新增商品" }).click();
  const editor = page.getByRole("dialog", { name: "新增商品" });
  await expect(editor).toHaveCSS("opacity", "1");
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
  await expect(editor.locator("#description textarea")).toBeInViewport();
  await expect(
    editor.getByRole("button", { name: "保存商品" }),
  ).toBeInViewport();
  await expect(editor.getByText("新增商品", { exact: true })).toBeInViewport();
  await page.screenshot({ path: "../.local/product-dialog-mobile.png" });
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
  await page.screenshot({ path: "../.local/order-mobile.png", fullPage: true });
  await page.goto(`${base}/admin/products`);
  await expect(page.locator(".n-spin-content--spinning")).toHaveCount(0);
  await expect(page.locator(".n-spin-content").first()).toHaveCSS(
    "opacity",
    "1",
  );
  await page.screenshot({ path: "../.local/admin-mobile.png", fullPage: true });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(base);
  await expect(page.locator(".n-spin-content--spinning")).toHaveCount(0);
  await expect(page.locator(".n-spin-content").first()).toHaveCSS(
    "opacity",
    "1",
  );
  await page.screenshot({ path: "../.local/shop-desktop.png", fullPage: true });
  await page.goto(`${base}/admin/orders`);
  await expect(page.locator(".order-row").first()).toBeVisible();
  await expect(page.locator(".n-spin-content")).toHaveCSS("opacity", "1");
  await page.screenshot({
    path: "../.local/orders-desktop.png",
    fullPage: true,
  });
  const blocked = await page.context().newPage();
  await blocked.addInitScript(() => {
    Storage.prototype.setItem = () => {
      throw new Error("存储不可用");
    };
  });
  await blocked.goto(base);
  await blocked
    .locator(".product-card")
    .filter({ hasText: "卡密" })
    .getByRole("button", { name: "查看商品" })
    .click();
  await blocked.getByRole("button", { name: "创建订单", exact: true }).click();
  await expect(
    blocked.getByRole("button", { name: "前往 HashPay 付款" }),
  ).toBeDisabled();
  await blocked.getByText("我已手动保存领取链接", { exact: true }).click();
  await expect(
    blocked.getByRole("button", { name: "前往 HashPay 付款" }),
  ).toBeEnabled();
  await blocked.close();
  await page.goto(`${base}/admin/orders`);
  const paidRow = page.locator(".order-row").filter({ hasText: orderID });
  await paidRow.getByRole("button", { name: orderID, exact: true }).click();
  await page.getByRole("button", { name: "刷新状态", exact: true }).click();
  await expect(page.getByText("订单状态已更新", { exact: true })).toBeVisible();
  await expect(page.locator(".form-modal .delivery-item pre")).toContainText(
    "卡密-",
  );
  await expect(page.locator(".form-modal")).toHaveCSS("opacity", "1");
  await page.screenshot({
    path: "../.local/order-actions-desktop.png",
    fullPage: true,
  });
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
  expect(errors).toEqual([]);
});
