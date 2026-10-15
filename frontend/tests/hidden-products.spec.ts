import {
  test,
  expect,
  createInventory,
  selectProductInventory,
  importInventory,
} from "./fixture";
import { placeTestOrder } from "./checkout";

test("隐藏商品通过链接购买，取消隐藏后恢复首页入口", async ({
  page,
  request,
}) => {
  const base = process.env.TINYFAKA_TEST_URL!;
  const name = "链接专用卡密";
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`${base}/admin/login`);
  await page.locator("#password input").fill("test-password-only");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  const pool = await createInventory(page, "链接专用库存");
  await page.getByRole("menuitem", { name: "商品管理", exact: true }).click();
  await page.getByRole("button", { name: "新增商品", exact: true }).click();
  const editor = page.getByRole("dialog", { name: "新增商品", exact: true });
  await editor.locator("#product-name input").fill(name);
  await editor.locator("#price input").fill("6.20");
  await selectProductInventory(page, pool);
  await editor.locator("#active").click();
  await editor.getByRole("switch", { name: "隐藏商品", exact: true }).click();
  const saved = page.waitForResponse(
    (response) =>
      response.url() === `${base}/api/admin/products` &&
      response.request().method() === "POST",
  );
  await editor.getByRole("button", { name: "保存商品", exact: true }).click();
  const product = await (await saved).json();
  expect(product.hidden).toBe(true);
  expect(product.active).toBe(true);
  const row = page.getByRole("row").filter({ hasText: name });
  await expect(row).toContainText("隐藏商品");
  await importInventory(page, pool, "HIDDEN-001\nHIDDEN-002\nHIDDEN-003");
  await page.goto(`${base}/admin/products`);
  await expect(row).toContainText("3 / 0 / 0");
  await page.reload();
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(
    page.getByRole("switch", { name: "隐藏商品", exact: true }),
  ).toHaveAttribute("aria-checked", "true");
  await page.screenshot({
    path: "../.local/hidden-product-editor.png",
    animations: "disabled",
  });
  await page.getByRole("button", { name: "取消", exact: true }).click();
  const link = row.getByRole("link", { name, exact: true });
  const path = await link.getAttribute("href");
  expect(path).toBe(`/products/${product.id}`);
  await link.click();
  await expect(page.locator(".product-summary")).toContainText(name);
  await expect(page.getByRole("link", { name: "返回商城" })).toHaveCount(0);
  await page.goto(base);
  await expect(page.locator(".product-grid")).toBeVisible();
  await expect(
    page.locator(".product-card").filter({ hasText: name }),
  ).toHaveCount(0);
  await page.goto(`${base}${path}`);
  await page.getByRole("button", { name: "下单", exact: true }).click();
  const purchase = page.getByRole("dialog", { name: "确认下单", exact: true });
  await expect(purchase.getByText(/库存/)).toHaveCount(0);
  await purchase.getByLabel("购买数量").fill("2");
  await purchase.getByLabel("购买数量").blur();
  await expect(purchase.locator(".purchase-total")).toContainText("12.40 CNY");
  await page.screenshot({
    path: "../.local/hidden-product-purchase.png",
    animations: "disabled",
  });
  await page.setViewportSize({ width: 390, height: 900 });
  await expect(purchase).toBeInViewport();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth + 1,
    ),
  ).toBeFalsy();
  const { orderID } = await placeTestOrder(page);
  await page.goto(`${base}/claim`);
  await expect(
    page.getByRole("button", { name: "前往 HashPay 付款" }),
  ).toBeVisible({ timeout: 15_000 });
  expect(
    (await request.post(`${base}/__test/pay/${orderID}`)).ok(),
  ).toBeTruthy();
  await page.reload();
  await expect(page.locator(".delivery-item pre")).toHaveCount(2);
  for (const result of await page.locator(".delivery-item pre").all()) {
    await expect(result).toContainText("HIDDEN-");
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`${base}/admin/products`);
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  await page.getByRole("switch", { name: "隐藏商品", exact: true }).click();
  await page.getByRole("button", { name: "保存商品", exact: true }).click();
  await expect(row).not.toContainText("隐藏商品");
  await page.goto(base);
  const card = page.locator(".product-card").filter({ hasText: name });
  await expect(card).toBeVisible();
  await card.getByRole("link", { name: "详情", exact: true }).click();
  await expect(page.getByRole("link", { name: "返回商城" })).toBeVisible();
  // 恢复隐藏，避免本用例的库存进入共享测试商城，影响后续购买用例。
  await page.goto(`${base}/admin/products`);
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  await page.getByRole("switch", { name: "隐藏商品", exact: true }).click();
  await page.getByRole("button", { name: "保存商品", exact: true }).click();
  await expect(row).toContainText("隐藏商品");
  expect(errors).toEqual([]);
});
