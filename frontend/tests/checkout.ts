import { expect, type Page } from "./fixture";
import type { Order } from "../src/api";

export async function stubPaymentPage(page: Page) {
  const base = process.env.HASHDROP_TEST_URL!;
  // 仅替换第三方托管页面；创建、签名查询、库存及交付均使用真实 Go 后端与测试网关。
  await page.route("**/pay/gw-*", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: `<html><head><title>HashPay test</title></head><body><h1>HashPay</h1><a href="${base}/claim">Return to store</a></body></html>`,
    }),
  );
}

export async function placeTestOrder(page: Page, customKey?: string) {
  const base = process.env.HASHDROP_TEST_URL!;
  await stubPaymentPage(page);
  const field = page.locator("#purchase-key");
  if (customKey !== undefined) await field.fill(customKey);
  else if (!(await field.inputValue()))
    await page.getByRole("button", { name: /^(生成|Generate)$/ }).click();
  const key = await field.inputValue();
  const created = page.waitForResponse(
    (response) =>
      response.url() === `${base}/api/orders` &&
      response.request().method() === "POST",
  );
  await page
    .getByRole("button", {
      name: /^(前往支付|继续获取支付链接|Proceed to payment|Retry payment link)$/,
    })
    .click();
  const response = await created;
  expect(response.status()).toBe(201);
  await expect(page).toHaveURL(/\/pay\/gw-/, { timeout: 15_000 });
  // 已有支付链接时会立即跳转，浏览器可能已释放原响应体；重新读取真实持久化结果。
  const lookup = await page.request.post(`${base}/api/orders/lookup`, {
    data: { key },
  });
  expect(lookup.ok()).toBeTruthy();
  const { order } = (await lookup.json()) as { order: Order };
  expect(order.id).toBe(response.request().postDataJSON().request_id);
  expect(page.url()).not.toContain(key);
  return {
    key,
    order,
    orderID: order.id,
    receipt: `${base}/orders/${order.id}#token=${encodeURIComponent(key)}`,
  };
}
