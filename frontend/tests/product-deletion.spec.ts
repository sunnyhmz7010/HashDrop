import { test, expect } from "./fixture";
import { placeTestOrder } from "./checkout";

test("删除商品保留订单和领取，失败可重试", async ({ page, request }) => {
  const base = process.env.TINYFAKA_TEST_URL!;
  const name = "删除验证卡密";
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`${base}/admin/login`);
  await page.locator("#password input").fill("test-password-only");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("menuitem", { name: "商品与库存", exact: true }).click();
  await page.getByRole("button", { name: "新增商品", exact: true }).click();
  await page.locator("#product-name input").fill(name);
  await page.locator("#price input").fill("7.50");
  await page.locator("#active").click();
  const saved = page.waitForResponse(
    (response) =>
      response.url() === `${base}/api/admin/products` &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "保存商品", exact: true }).click();
  const product = await (await saved).json();
  const row = page.getByRole("row").filter({ hasText: name });
  await row.getByRole("button", { name: "导入", exact: true }).click();
  await page
    .getByPlaceholder("每行一份库存")
    .fill("DELETE-001\nDELETE-002\nDELETE-003\nDELETE-004");
  await page.getByRole("button", { name: "导入库存", exact: true }).click();
  await expect(row).toContainText("4 / 0 / 0");

  const receipts: { url: string; id: string }[] = [];
  for (let index = 0; index < 2; index++) {
    await page.goto(`${base}/products/${product.id}`);
    await page.getByRole("button", { name: "下单", exact: true }).click();
    const placed = await placeTestOrder(
      page,
      `delete-${index}-`.padEnd(43, "k"),
    );
    await page.goto(placed.receipt);
    await expect(
      page.getByRole("button", { name: "前往 HashPay 付款" }),
    ).toBeVisible({ timeout: 15_000 });
    const receipt = {
      url: placed.receipt,
      id: placed.orderID,
    };
    receipts.push(receipt);
    if (index === 0) {
      expect(
        (await request.post(`${base}/__test/pay/${receipt.id}`)).ok(),
      ).toBeTruthy();
      await page.reload();
      await expect(page.locator(".delivery-item pre")).toContainText("DELETE-");
    }
  }
  await page.goto(`${base}/admin/products`);
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  const editor = page.getByRole("dialog", { name: "编辑商品", exact: true });
  await expect(editor.locator(".publication-state")).toContainText("上架");
  await page.screenshot({
    path: "../.local/product-publication-desktop.png",
    animations: "disabled",
  });
  await page.setViewportSize({ width: 390, height: 900 });
  await expect(editor).toBeInViewport();
  await expect(
    editor.getByRole("button", { name: "保存商品", exact: true }),
  ).toBeInViewport();
  await page.screenshot({
    path: "../.local/product-publication-mobile.png",
    animations: "disabled",
  });
  await editor.getByRole("button", { name: "取消", exact: true }).click();
  await page.setViewportSize({ width: 1440, height: 1000 });

  let deletes = 0;
  let finishDelete = () => {};
  const pendingDelete = new Promise<void>((resolve) => {
    finishDelete = resolve;
  });
  await page.route(`**/api/admin/products/${product.id}`, async (route) => {
    if (route.request().method() === "DELETE") {
      deletes++;
      await pendingDelete;
      await route.fulfill({ status: 503, json: { error: "商品删除暂时失败" } });
    } else await route.continue();
  });
  await row.getByRole("button", { name: "删除", exact: true }).click();
  const confirmation = page.locator(".n-dialog");
  await expect(confirmation).toContainText(name);
  await confirmation.getByRole("button", { name: "取消", exact: true }).click();
  await expect(row).toBeVisible();
  expect(deletes).toBe(0);
  await row.getByRole("button", { name: "删除", exact: true }).click();
  await page.screenshot({
    path: "../.local/product-delete-confirm.png",
    animations: "disabled",
  });
  await confirmation.getByRole("button", { name: "删除", exact: true }).click();
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
    page.getByText("商品删除暂时失败", { exact: true }),
  ).toBeVisible();
  await expect(row).toBeVisible();
  await expect(
    confirmation.getByRole("button", { name: "删除", exact: true }),
  ).toBeEnabled();
  await page.unroute(`**/api/admin/products/${product.id}`);
  await confirmation.getByRole("button", { name: "删除", exact: true }).click();
  await expect(row).toHaveCount(0);
  await page.reload();
  await expect(row).toHaveCount(0);
  await page.goto(base);
  await expect(page.locator(".product-grid")).toBeVisible();
  await expect(
    page.locator(".product-card").filter({ hasText: name }),
  ).toHaveCount(0);
  await page.goto(`${base}/products/${product.id}`);
  await expect(page.getByRole("alert")).toContainText("商品不存在或已下架");
  await expect(
    page.getByRole("button", { name: "下单", exact: true }),
  ).toHaveCount(0);
  expect(
    (await request.post(`${base}/__test/pay/${receipts[1].id}`)).ok(),
  ).toBeTruthy();
  for (const receipt of receipts) {
    await page.goto(receipt.url);
    await expect(page.locator(".delivery-item pre")).toContainText("DELETE-");
  }
  await page.goto(`${base}/admin/orders`);
  for (const receipt of receipts) {
    await expect(
      page.locator(".order-row").filter({ hasText: receipt.id }),
    ).toContainText(name);
  }
  expect(errors).toEqual([]);
});
