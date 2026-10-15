import {
  loginAdmin,
  test,
  expect,
  selectProductInventory,
  type Page,
} from "./fixture";
import type { Inventory, Product } from "../src/api";
import { placeTestOrder } from "./checkout";

test("独立库存池共享、商品改绑与历史订单交付", async ({ page, request }) => {
  test.setTimeout(90_000);
  const base = process.env.TINYFAKA_TEST_URL!;
  await loginAdmin(page);
  await page.getByRole("menuitem", { name: "库存管理", exact: true }).click();

  async function createPool(
    name: string,
    content?: string,
  ): Promise<Inventory> {
    await page.goto(`${base}/admin/inventories`);
    await page.getByRole("button", { name: "新增库存池", exact: true }).click();
    const editor = page.getByRole("dialog", {
      name: "新增库存池",
      exact: true,
    });
    await editor.locator("#inventory-name input").fill(name);
    if (content !== undefined) {
      await editor.locator("#inventory-kind").click();
      await page
        .locator(".n-base-select-option")
        .getByText("固定文本", { exact: true })
        .click();
      await editor
        .getByRole("button", { name: "保存库存池", exact: true })
        .click();
      await expect(editor.getByRole("alert")).toContainText("固定内容不能为空");
      await expect(editor.locator("#inventory-name input")).toHaveValue(name);
      await editor.locator("#static-content textarea").fill(content);
    }
    const saved = page.waitForResponse(
      (response) =>
        response.url() === `${base}/api/admin/inventories` &&
        response.request().method() === "POST",
    );
    await editor
      .getByRole("button", { name: "保存库存池", exact: true })
      .click();
    const response = await saved;
    expect(response.ok()).toBeTruthy();
    await expect(editor).toHaveCount(0);
    return response.json();
  }
  const pool = await createPool("多商品共享卡密库存");
  const poolRow = page.getByRole("row").filter({ hasText: pool.id });
  await expect(poolRow).toContainText("0 个商品");
  await poolRow.getByRole("button", { name: "导入", exact: true }).click();
  const importer = page.getByRole("dialog", {
    name: `导入库存 · ${pool.name}`,
    exact: true,
  });
  const content = "SHARED-001\nSHARED-002\nSHARED-001";
  await importer.getByLabel("库存内容", { exact: true }).fill(content);
  const stockURL = `**/api/admin/inventories/${pool.id}/stock`;
  await page.route(stockURL, (route) =>
    route.fulfill({ status: 503, json: { error: "库存导入暂时失败" } }),
  );
  await importer.getByRole("button", { name: "导入库存", exact: true }).click();
  await expect(importer.getByRole("alert")).toContainText("库存导入暂时失败");
  await expect(importer.getByLabel("库存内容", { exact: true })).toHaveValue(
    content,
  );
  await page.unroute(stockURL);
  await importer.getByRole("button", { name: "导入库存", exact: true }).click();
  await expect(importer).toHaveCount(0);
  await expect(poolRow).toContainText("2 / 0 / 0");
  await page.reload();
  await expect(poolRow).toContainText("2 / 0 / 0");

  async function createProduct(name: string, price: string): Promise<Product> {
    await page.goto(`${base}/admin/products`);
    await page.getByRole("button", { name: "新增商品", exact: true }).click();
    const editor = page.getByRole("dialog", { name: "新增商品", exact: true });
    await expect(
      editor.locator("#inventory-kind, #static-content"),
    ).toHaveCount(0);
    await editor.locator("#product-name input").fill(name);
    await editor.locator("#price input").fill(price);
    await selectProductInventory(page, pool);
    await editor.locator("#active").click();
    await editor.getByRole("switch", { name: "隐藏商品", exact: true }).click();
    const saved = page.waitForResponse(
      (response) =>
        response.url() === `${base}/api/admin/products` &&
        response.request().method() === "POST",
    );
    await editor.getByRole("button", { name: "保存商品", exact: true }).click();
    const response = await saved;
    expect(response.ok()).toBeTruthy();
    await expect(editor).toHaveCount(0);
    return response.json();
  }
  const first = await createProduct("共享库存商品 A", "5.00");
  const second = await createProduct("共享库存商品 B", "8.00");
  const firstRow = page.getByRole("row").filter({ hasText: first.name });
  const secondRow = page.getByRole("row").filter({ hasText: second.name });
  for (const row of [firstRow, secondRow]) {
    await expect(row).toContainText(pool.name);
    await expect(row).toContainText("2 / 0 / 0");
    await expect(
      row.getByRole("button", { name: "导入", exact: true }),
    ).toHaveCount(0);
  }
  await page.screenshot({
    path: "../.local/shared-inventory-products.png",
    fullPage: true,
    animations: "disabled",
  });

  async function buy(product: Product) {
    await page.goto(`${base}/products/${product.id}`);
    await page.getByRole("button", { name: "下单", exact: true }).click();
    const placed = await placeTestOrder(page);
    return {
      key: placed.key,
      id: placed.orderID,
    };
  }
  async function viewOrder(order: { key: string }) {
    await page.goto(`${base}/claim`);
    await page
      .getByRole("button", { name: "查询其他订单", exact: true })
      .click();
    await page.getByLabel("领取密钥", { exact: true }).fill(order.key);
    await page.getByRole("button", { name: "查看订单", exact: true }).click();
  }
  const pending = await buy(first);
  await page.goto(`${base}/admin/products`);
  await expect(firstRow).toContainText("1 / 1 / 0");
  await expect(secondRow).toContainText("1 / 1 / 0");
  const fixed = await createPool("改绑固定内容库存", "REBOUND-CONTENT");
  await page.goto(`${base}/admin/products`);
  await firstRow.getByRole("button", { name: "编辑", exact: true }).click();
  await selectProductInventory(page, fixed);
  await page.getByRole("button", { name: "保存商品", exact: true }).click();
  await expect(firstRow).toContainText(fixed.name);
  await page.reload();
  await expect(firstRow).toContainText(fixed.name);
  await expect(secondRow).toContainText(pool.name);

  expect(
    (await request.post(`${base}/__test/pay/${pending.id}`)).ok(),
  ).toBeTruthy();
  await viewOrder(pending);
  await expect(page.locator(".delivery-item pre")).toHaveText(
    /^SHARED-00[12]$/,
  );
  const originalContent = await page.locator(".delivery-item pre").innerText();
  const fresh = await buy(first);
  expect(
    (await request.post(`${base}/__test/pay/${fresh.id}`)).ok(),
  ).toBeTruthy();
  await viewOrder(fresh);
  await expect(page.locator(".delivery-item pre")).toHaveText(
    "REBOUND-CONTENT",
  );
  const another = await buy(second);
  expect(
    (await request.post(`${base}/__test/pay/${another.id}`)).ok(),
  ).toBeTruthy();
  await viewOrder(another);
  await expect(page.locator(".delivery-item pre")).toHaveText(
    /^SHARED-00[12]$/,
  );
  expect(await page.locator(".delivery-item pre").innerText()).not.toBe(
    originalContent,
  );
  await page.goto(`${base}/admin/inventories`);
  await expect(poolRow).toContainText("0 / 0 / 2");
  await expect(poolRow).toContainText("1 个商品");
  const session = await (
    await page.request.get(`${base}/api/admin/session`)
  ).json();
  const removed = await page.request.delete(
    `${base}/api/admin/products/${first.id}`,
    { headers: { Origin: base, "X-CSRF-Token": session.csrf } },
  );
  expect(removed.ok()).toBeTruthy();
  await page.reload();
  await expect(
    page.getByRole("row").filter({ hasText: fixed.id }),
  ).toContainText("0 个商品");
  await viewOrder(pending);
  await expect(page.locator(".delivery-item pre")).toHaveText(originalContent);

  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`${base}/admin/inventories`);
    await expect(poolRow).toBeVisible();
    await verifyViewport(page);
    await page.screenshot({
      path: `../.local/independent-inventory-${width}.png`,
      fullPage: true,
      animations: "disabled",
    });
    await page.getByRole("button", { name: "新增库存池", exact: true }).click();
    const editor = page.getByRole("dialog", {
      name: "新增库存池",
      exact: true,
    });
    await expect(
      editor.getByRole("button", { name: "保存库存池", exact: true }),
    ).toBeInViewport();
    await verifyViewport(page);
    await page.screenshot({
      path: `../.local/inventory-editor-${width}.png`,
      animations: "disabled",
    });
    await editor.getByRole("button", { name: "取消", exact: true }).click();
    await page.goto(`${base}/admin/products`);
    await secondRow.getByRole("button", { name: "编辑", exact: true }).click();
    await expect(page.locator("#product-inventory")).toContainText(pool.name);
    await expect(
      page.getByRole("button", { name: "保存商品", exact: true }),
    ).toBeInViewport();
    await verifyViewport(page);
    await page.screenshot({
      path: `../.local/product-inventory-selector-${width}.png`,
      animations: "disabled",
    });
    await page.getByRole("button", { name: "取消", exact: true }).click();
  }
});

async function verifyViewport(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth + 1,
    ),
  ).toBeFalsy();
}
