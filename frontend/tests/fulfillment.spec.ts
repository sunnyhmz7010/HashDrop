import {
  test,
  expect,
  loginAdmin,
  createInventory,
  importInventory,
  bindInventoryProducts,
  claimContents,
  testPlan,
} from "./fixture";
import { placeTestOrder } from "./checkout";
import type { Product } from "../src/api";

test("组合交付包含卡密、文字和外链，移除附件入口并主动领取", async ({
  page,
  request,
}) => {
  const base = process.env.HASHDROP_TEST_URL!;
  await loginAdmin(page);
  const pool = await createInventory(page, "组合交付独占池");
  await importInventory(page, pool, "COMPOSED-CODE-001\nCOMPOSED-CODE-002");
  await page.goto(`${base}/center/products`);
  await page.getByRole("button", { name: "新增商品", exact: true }).click();
  await page.locator("#product-name input").fill("文字外链组合交付");
  await page.locator("#price input").fill("12.00");
  await page.locator("#product-min-quantity").fill("2");
  await page.locator("#product-max-quantity").fill("3");
  await page.locator("#active").click();
  await page.getByRole("button", { name: "添加文字", exact: true }).click();
  await page
    .locator(".content-editor textarea")
    .fill(
      `## 使用指南\n\n请保管本份内容。\n\n![使用说明](${base}/__test/media/image.svg)`,
    );
  await expect(page.getByRole("button", { name: /^上传/ })).toHaveCount(0);
  await expect(page.locator('.content-editor input[type="file"]')).toHaveCount(
    0,
  );
  await page.getByRole("button", { name: "添加链接", exact: true }).click();
  const link = page.locator('[data-content-type="link"]');
  await link.locator('input[id^="content-label-"]').fill("操作手册");
  await link.locator('input[type="url"]').fill("https://example.com/guide");
  const saved = page.waitForResponse(
    (response) =>
      response.url() === `${base}/api/admin/products` &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "保存商品", exact: true }).click();
  const saveResponse = await saved;
  expect(saveResponse.ok(), await saveResponse.text()).toBeTruthy();
  const product = (await saveResponse.json()) as Product;
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await bindInventoryProducts(page, pool, [product.id]);
  expect(product.delivery_plan?.blocks.map((block) => block.type)).toEqual([
    "stock",
    "text",
    "link",
  ]);
  expect(
    (await (await request.get(`${base}/api/products/${product.id}`)).json())
      .delivery_plan,
  ).toBeUndefined();

  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto(`${base}/products/${product.id}`);
  await page.getByRole("button", { name: "下单", exact: true }).click();
  const purchase = await placeTestOrder(page);
  expect(purchase.order.quantity).toBe(2);
  expect(
    (await request.post(`${base}/__test/pay/${purchase.orderID}`)).ok(),
  ).toBeTruthy();
  const lookup = await request.post(`${base}/api/orders/lookup`, {
    data: { key: purchase.key },
  });
  const metadata = await lookup.json();
  expect(
    metadata.deliveries.every(
      (delivery: { content?: unknown }) => delivery.content === undefined,
    ),
  ).toBeTruthy();
  let claimRequests = 0;
  page.on("request", (event) => {
    if (event.url().endsWith("/claim")) claimRequests++;
  });
  await page.goto(purchase.receipt);
  await expect(
    page.getByRole("button", { name: "领取内容", exact: true }),
  ).toHaveCount(2);
  expect(claimRequests).toBe(0);
  await claimContents(page);
  expect(claimRequests).toBe(2);
  const receipts = page.locator(".delivery-item");
  const codes = await receipts
    .locator('[data-content-type="text"] .markdown-content')
    .allTextContents();
  expect(
    codes
      .filter((text) => text.startsWith("COMPOSED-CODE-"))
      .map((text) => text.trim())
      .sort(),
  ).toEqual(["COMPOSED-CODE-001", "COMPOSED-CODE-002"]);
  await expect(receipts.getByRole("heading", { name: "使用指南" })).toHaveCount(
    2,
  );
  await expect(receipts.getByRole("link", { name: "操作手册" })).toHaveCount(2);
  await expect
    .poll(() =>
      receipts
        .locator("img")
        .evaluateAll(
          (images) =>
            images.length === 2 &&
            images.every(
              (image) => (image as HTMLImageElement).naturalWidth > 0,
            ),
        ),
    )
    .toBeTruthy();
  expect((await request.post(`${base}/api/admin/assets`)).status()).toBe(405);
  expect((await request.get(`${base}/api/admin/assets/removed`)).status()).toBe(
    404,
  );
  expect(
    (
      await request.get(
        `${base}/api/orders/${purchase.orderID}/deliveries/${metadata.deliveries[0].id}/assets/removed`,
      )
    ).status(),
  ).toBe(404);
  expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain(
    "COMPOSED-CODE",
  );
  await page.getByRole("button", { name: "刷新状态", exact: true }).click();
  expect(claimRequests).toBe(2);
  await page.reload();
  await expect(
    page.getByRole("button", { name: "领取内容", exact: true }),
  ).toHaveCount(2);
  expect(claimRequests).toBe(2);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth + 1,
    ),
  ).toBeFalsy();
});

test("失败交付按份补交，响应丢失后恢复原结果且买家显式领取", async ({
  page,
  request,
  context,
}) => {
  const base = process.env.HASHDROP_TEST_URL!;
  await loginAdmin(page);
  const pool = await createInventory(page, "补交测试预留池");
  await importInventory(page, pool, "RESERVED-001\nRESERVED-002");
  const session = await (
    await page.request.get(`${base}/api/admin/session`)
  ).json();
  const created = await page.request.post(`${base}/api/admin/products`, {
    headers: { Origin: base, "X-CSRF-Token": session.csrf },
    data: {
      name: "失败补交浏览器商品",
      price: 1200,
      currency: "CNY",
      active: true,
      min_quantity: 2,
      max_quantity: 3,
      delivery_plan: testPlan(),
    },
  });
  expect(created.ok(), await created.text()).toBeTruthy();
  const product: Product = await created.json();
  await bindInventoryProducts(page, pool, [product.id]);
  await page.goto(`${base}/products/${product.id}`);
  await page.getByRole("button", { name: "下单", exact: true }).click();
  const purchase = await placeTestOrder(page);
  expect(
    (await request.post(`${base}/__test/paid-only/${purchase.orderID}`)).ok(),
  ).toBeTruthy();
  const buyer = await context.newPage();
  await buyer.goto(purchase.receipt);
  await expect(
    buyer.getByRole("button", { name: "领取内容", exact: true }),
  ).toHaveCount(0);
  await page.goto(`${base}/center/orders`);
  await page.locator("#orders-search").fill(purchase.orderID);
  await page.getByRole("button", { name: "筛选", exact: true }).click();
  await page
    .locator(".order-row")
    .filter({ hasText: product.name })
    .getByRole("button", { name: purchase.orderID, exact: true })
    .click();
  for (const [index, content] of [
    "第一份凭证-001",
    "第二份凭证-002",
  ].entries()) {
    await page
      .getByRole("button", { name: "交付此份商品", exact: true })
      .first()
      .click();
    const form = page.locator(".order-modal-body form");
    await form.locator(".content-editor textarea").fill(content);
    if (index === 0)
      await page.route(
        `**/api/admin/orders/${purchase.orderID}/fulfill`,
        async (route) => {
          const response = await route.fetch();
          expect(response.ok(), await response.text()).toBeTruthy();
          await route.abort("failed");
        },
        { times: 1 },
      );
    await form
      .getByRole("button", { name: "交付此份商品", exact: true })
      .click();
    if (index === 0) {
      await expect(
        page.getByRole("alert").filter({ hasText: "网络" }),
      ).toBeVisible();
      await expect(form.locator("textarea")).toHaveValue(content);
      await form
        .getByRole("button", { name: "交付此份商品", exact: true })
        .click();
    }
    await expect(
      page.locator(".order-modal-body .delivery-item .markdown-content"),
    ).toHaveCount(index + 1);
    await buyer.reload();
    await claimContents(buyer);
    await expect(buyer.locator(".delivery-item .markdown-content")).toHaveCount(
      index + 1,
    );
    await expect(
      buyer.locator(".delivery-item .markdown-content").nth(index),
    ).toHaveText(content);
    await expect(
      buyer.getByText(`已交付 ${index + 1} / 2 份`, { exact: true }),
    ).toBeVisible();
  }
  await expect(
    page.getByRole("button", { name: "交付此份商品", exact: true }),
  ).toHaveCount(0);
  await buyer.close();
});
