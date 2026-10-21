import { test, expect, loginAdmin } from "./fixture";
import { placeTestOrder } from "./checkout";
import type { Product } from "../src/api";

test("人工商品配置、购买、分份交付与密钥领取", async ({
  page,
  request,
  context,
}) => {
  const base = process.env.HASHDROP_TEST_URL!;
  await loginAdmin(page);
  await page.goto(`${base}/center/products`);
  await page.getByRole("button", { name: "新增商品", exact: true }).click();
  await page.locator("#product-name input").fill("人工交付浏览器商品");
  await page.locator("#product-delivery-mode").click();
  await page
    .locator(".n-base-select-option")
    .filter({ hasText: "人工交付" })
    .click();
  await expect(page.locator("#product-inventory")).toHaveCount(0);
  await page.locator("#price input").fill("12.00");
  await page.locator("#product-min-quantity").fill("2");
  await page.locator("#product-max-quantity").fill("3");
  await page
    .locator("#product-delivery-note textarea")
    .fill("两小时内处理，完成后凭密钥领取。");
  await page.locator("#product-buyer-note-label input").fill("接收账号");
  await page.locator("#product-buyer-note-required").click();
  await page.locator("#active").click();
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
  await page
    .getByRole("row")
    .filter({ hasText: product.name })
    .getByRole("button", { name: "编辑", exact: true })
    .click();
  await expect(page.locator("#product-min-quantity")).toHaveValue("2");
  await expect(page.locator("#product-buyer-note-label input")).toHaveValue(
    "接收账号",
  );
  await page.getByRole("button", { name: "取消", exact: true }).click();

  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto(`${base}/products/${product.id}`);
  await page.getByRole("button", { name: "下单", exact: true }).click();
  await expect(page.locator("#purchase-quantity")).toHaveValue("2");
  await expect(
    page.getByRole("button", { name: "前往支付", exact: true }),
  ).toBeDisabled();
  await page.locator("#purchase-buyer-note").fill("收货账号@example.com");
  let interruptedOrderID = "";
  await page.route(
    "**/api/orders",
    async (route) => {
      const response = await route.fetch();
      expect(response.status()).toBe(201);
      interruptedOrderID = (await response.json()).order.id;
      await route.abort("failed");
    },
    { times: 1 },
  );
  await page.getByRole("button", { name: "前往支付", exact: true }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "网络" }),
  ).toBeVisible();
  await page.reload();
  await page.getByRole("button", { name: "下单", exact: true }).click();
  await expect(page.locator("#purchase-buyer-note")).toHaveValue(
    "收货账号@example.com",
  );
  await expect(page.locator("#purchase-buyer-note")).toBeDisabled();
  const purchase = await placeTestOrder(page);
  expect(purchase.orderID).toBe(interruptedOrderID);
  expect(purchase.order.quantity).toBe(2);
  expect(purchase.order.total).toBe(2400);
  expect(
    (await request.post(`${base}/__test/pay/${purchase.orderID}`)).ok(),
  ).toBeTruthy();
  await page.goto(purchase.receipt);
  await expect(
    page.getByText(
      "付款已确认，等待商家交付。已完成的部分可在下方领取，请保留领取密钥。",
    ),
  ).toBeVisible();

  const buyer = await context.newPage();
  await buyer.goto(purchase.receipt);
  await page.goto(`${base}/center/orders`);
  await page.locator("#orders-search").fill(purchase.orderID);
  await page.getByRole("button", { name: "筛选", exact: true }).click();
  const row = page.locator(".order-row").filter({ hasText: product.name });
  await row
    .getByRole("button", { name: purchase.orderID, exact: true })
    .click();
  await expect(
    page.locator(".order-note").filter({ hasText: "收货账号@example.com" }),
  ).toBeVisible();
  for (const [index, content] of [
    "第一份凭证-001",
    "第二份凭证-002",
  ].entries()) {
    await page
      .getByRole("button", { name: "交付此份商品", exact: true })
      .first()
      .click();
    await page.locator("textarea[id^='delivery-content-']").fill(content);
    const form = page
      .locator("form")
      .filter({ has: page.locator("textarea[id^='delivery-content-']") });
    if (index === 0)
      await page.route(
        `**/api/admin/orders/${purchase.orderID}/fulfill`,
        async (route) => {
          const response = await route.fetch();
          expect(response.ok()).toBeTruthy();
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
      page.locator(".order-modal-body .delivery-item pre"),
    ).toHaveCount(index + 1);
    await buyer.reload();
    await expect(buyer.locator(".delivery-item pre")).toHaveCount(index + 1);
    await expect(buyer.locator(".delivery-item pre").nth(index)).toHaveText(
      content,
    );
    await expect(
      buyer.getByText(`已交付 ${index + 1} / 2 份`, { exact: true }),
    ).toBeVisible();
  }
  await expect(
    page.getByRole("button", { name: "交付此份商品", exact: true }),
  ).toHaveCount(0);
  expect(
    await buyer.evaluate(
      () => document.documentElement.scrollWidth > innerWidth + 1,
    ),
  ).toBeFalsy();
  await buyer.close();
});
