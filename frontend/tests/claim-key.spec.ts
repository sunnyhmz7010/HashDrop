import { test, expect } from "./fixture";
import { placeTestOrder, stubPaymentPage } from "./checkout";
import type { Product } from "../src/api";

test("密钥生成、重复候选和跨设备领取", async ({ page, request }) => {
  const base = process.env.HASHDROP_TEST_URL!;
  const products: Product[] = await (
    await request.get(`${base}/api/products`)
  ).json();
  const product = products.find((item) => item.kind === "static")!;
  const key = "自定密钥 AbC🔑-2026";
  await page.goto(`${base}/products/${product.id}`);
  await page.getByRole("button", { name: "下单", exact: true }).click();
  const first = await placeTestOrder(page, key);
  await page.goto(`${base}/products/${product.id}`);
  await page.getByRole("button", { name: "下单", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "确认下单", exact: true });
  await dialog.getByLabel("领取密钥", { exact: true }).fill(key);
  await dialog.getByRole("button", { name: "前往支付", exact: true }).click();
  await expect(dialog.getByRole("alert")).toContainText(
    "领取密钥已被使用，请更换或选择派生密钥",
  );
  const candidate = dialog.locator(".key-suggestions button").first();
  const derived = (await candidate.innerText()).trim();
  await candidate.click();
  await expect(dialog.getByLabel("领取密钥", { exact: true })).toHaveValue(
    derived,
  );
  expect(derived).toContain(`${key}-`);
  await dialog.getByRole("button", { name: "生成", exact: true }).click();
  const generated = await dialog
    .getByLabel("领取密钥", { exact: true })
    .inputValue();
  expect(generated).toMatch(/^[a-z0-9]{24}$/);
  expect(generated).toMatch(/[a-z]/);
  expect(generated).toMatch(/[0-9]/);
  await page.evaluate(() =>
    Object.defineProperty(window, "isSecureContext", { value: false }),
  );
  await dialog.getByRole("button", { name: "复制", exact: true }).click();
  await expect(page.getByText("已复制", { exact: true })).toBeVisible();
  for (const width of [1440, 768, 390, 320]) {
    await page.setViewportSize({ width, height: width === 320 ? 450 : 900 });
    await expect(dialog).toBeInViewport();
    await expect(
      dialog.getByRole("button", { name: "前往支付", exact: true }),
    ).toBeInViewport();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBeFalsy();
  }
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  expect(
    (await request.post(`${base}/__test/pay/${first.orderID}`)).ok(),
  ).toBeTruthy();
  // 新页面不继承原页面 sessionStorage，模拟另一台设备只凭密钥领取。
  const other = await page.context().newPage();
  await other.goto(`${base}/claim`);
  await other.getByLabel("领取密钥", { exact: true }).fill("错误密钥");
  await other.getByRole("button", { name: "查看订单", exact: true }).click();
  await expect(
    other.getByText("订单或凭证无效", { exact: true }),
  ).toBeVisible();
  await expect(other.locator(".delivery-item")).toHaveCount(0);
  await other.getByLabel("领取密钥", { exact: true }).fill(key);
  await other.getByRole("button", { name: "查看订单", exact: true }).click();
  await expect(other.locator(".delivery-item pre")).toContainText("购买时内容");
  expect(other.url()).toBe(`${base}/claim`);
  await other.setViewportSize({ width: 390, height: 900 });
  await other.close();
});

test("下单响应中断后重试复用订单并直接支付", async ({ page, request }) => {
  const base = process.env.HASHDROP_TEST_URL!;
  const login = await request.post(`${base}/api/admin/login`, {
    headers: { Origin: base },
    data: { username: "admin", password: "test-password-only" },
  });
  expect(login.ok()).toBeTruthy();
  const headers = { Origin: base, "X-CSRF-Token": (await login.json()).csrf };
  const inventoryResponse = await request.post(
    `${base}/api/admin/inventories`,
    {
      headers,
      data: { name: "密钥重试专用库存", kind: "card" },
    },
  );
  expect(inventoryResponse.ok()).toBeTruthy();
  const inventory = await inventoryResponse.json();
  const productInput = {
    name: "断网恢复专用商品",
    price: 1234,
    currency: "CNY",
    inventory_id: inventory.id,
    active: true,
    hidden: true,
  };
  const created = await request.post(`${base}/api/admin/products`, {
    headers,
    data: productInput,
  });
  expect(created.ok()).toBeTruthy();
  const product: Product = await created.json();
  expect(
    (
      await request.post(
        `${base}/api/admin/inventories/${inventory.id}/stock`,
        {
          headers,
          data: { content: "RETRY-ONLY-CARD" },
        },
      )
    ).ok(),
  ).toBeTruthy();
  await page.goto(`${base}/products/${product.id}`);
  await page.getByRole("button", { name: "下单", exact: true }).click();
  await page.getByRole("button", { name: "生成", exact: true }).click();
  const key = await page.locator("#purchase-key").inputValue();
  let firstID = "",
    requestID = "";
  await page.route("**/api/orders", async (route) => {
    const input = route.request().postDataJSON();
    requestID = input.request_id;
    // 请求已被真实后端处理，但浏览器未收到结果。
    const response = await route.fetch();
    firstID = (await response.json()).order.id;
    await route.abort("connectionreset");
  });
  await page.getByRole("button", { name: "前往支付", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "继续获取支付链接", exact: true }),
  ).toBeEnabled();
  await expect(page.locator("#purchase-key")).toHaveValue(key);
  await expect(page.locator("#purchase-key")).toBeDisabled();
  await page.unroute("**/api/orders");
  await stubPaymentPage(page);
  expect(
    (
      await request.put(`${base}/api/admin/products/${product.id}`, {
        headers,
        data: { ...productInput, price: 500 },
      })
    ).ok(),
  ).toBeTruthy();
  // 刷新后也能恢复未确认请求，售罄时不得创建第二笔。
  await page.reload();
  await page.getByRole("button", { name: "下单", exact: true }).click();
  await expect(page.locator("#purchase-key")).toHaveValue(key);
  await expect(page.locator(".purchase-total")).toContainText("12.34 CNY");
  // 原订单已有快照，即使此时商品下架也应能恢复支付。
  await page
    .getByRole("dialog", { name: "确认下单", exact: true })
    .getByRole("button", { name: "取消", exact: true })
    .click();
  expect(
    (
      await request.put(`${base}/api/admin/products/${product.id}`, {
        headers,
        data: { ...productInput, price: 500, active: false },
      })
    ).ok(),
  ).toBeTruthy();
  await page.getByRole("button", { name: "下单", exact: true }).click();
  await expect(page.locator(".purchase-total")).toContainText("12.34 CNY");
  const retried = await placeTestOrder(page);
  expect(retried.orderID).toBe(firstID);
  expect(retried.orderID).toBe(requestID);
  expect(retried.order.total).toBe(1234);
  await page.getByRole("link", { name: "Return to store" }).click();
  await expect(
    page.getByRole("button", { name: "前往 HashPay 付款" }),
  ).toBeEnabled();
  expect(page.url()).toBe(`${base}/claim`);
});
