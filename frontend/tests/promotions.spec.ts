import {
  loginAdmin,
  test,
  expect,
  createInventory,
  bindInventoryProducts,
  type Locator,
} from "./fixture";
import type { Promotion, Product } from "../src/api";
import { placeTestOrder } from "./checkout";

test("优惠管理保存、叠加报价、价格变化确认与历史领取", async ({
  page,
  request,
}) => {
  test.setTimeout(90_000);
  const base = process.env.HASHDROP_TEST_URL!;
  await loginAdmin(page);
  await expect(
    page.getByRole("menuitem", { name: "优惠", exact: true }),
  ).toBeVisible();
  const session = await (
    await page.request.get(`${base}/api/admin/session`)
  ).json();
  const headers = { Origin: base, "X-CSRF-Token": session.csrf };
  const pool = await createInventory(
    page,
    "优惠验证交付库存",
    "static",
    "PROMOTION-CONTENT",
  );
  async function createProduct(name: string, hidden: boolean) {
    const response = await page.request.post(`${base}/api/admin/products`, {
      headers,
      data: {
        name,
        description: "优惠验证介绍",
        details: "",
        price: 10000,
        currency: "CNY",
        active: true,
        hidden,
      },
    });
    expect(response.ok()).toBeTruthy();
    return response.json() as Promise<Product>;
  }
  const first = await createProduct("优惠叠加商品", false),
    second = await createProduct("优惠范围备用商品", true);
  await bindInventoryProducts(page, pool, [first.id, second.id]);
  await page.getByRole("menuitem", { name: "优惠", exact: true }).click();
  async function select(form: Locator, selector: string, name: string) {
    await form.locator(selector).click();
    await page
      .locator(".n-base-select-option")
      .getByText(name, { exact: true })
      .click();
  }
  async function saveNew(form: Locator) {
    const saved = page.waitForResponse(
      (response) =>
        response.url() === `${base}/api/admin/promotions` &&
        response.request().method() === "POST",
    );
    await form.getByRole("button", { name: "保存优惠", exact: true }).click();
    const response = await saved;
    expect(response.ok()).toBeTruthy();
    await expect(form).toHaveCount(0);
    return (await response.json()).id as string;
  }
  await page.getByRole("button", { name: "新增优惠", exact: true }).click();
  const add = page.getByRole("dialog", { name: "新增优惠", exact: true });
  await add.locator("#promotion-name input").fill("常驻八折活动");
  await select(add, "#promotion-scope", "指定商品");
  await select(add, "#promotion-products", first.name);
  await page
    .locator(".n-base-select-option")
    .getByText(second.name, { exact: true })
    .click();
  await add.locator("#promotion-name input").click();
  await add
    .getByRole("switch", { name: "允许与其他优惠一同使用", exact: true })
    .click();
  await add.locator("#promotion-limit").fill("10");
  const activityID = await saveNew(add);
  await page.reload();
  const activity = page.getByRole("row").filter({ hasText: "常驻八折活动" });
  await expect(activity).toContainText(first.name);
  await expect(activity).toContainText(second.name);
  await expect(activity).toContainText("0 / 10");
  await activity.getByRole("button", { name: "编辑", exact: true }).click();
  const edit = page.getByRole("dialog", { name: "编辑优惠", exact: true });
  await expect(
    edit.getByRole("switch", { name: "允许与其他优惠一同使用", exact: true }),
  ).toHaveAttribute("aria-checked", "true");
  await expect(edit.locator("#promotion-products")).toContainText(second.name);
  await page.setViewportSize({ width: 390, height: 900 });
  await expect(
    edit.getByRole("button", { name: "保存优惠", exact: true }),
  ).toBeInViewport();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > innerWidth + 1,
    ),
  ).toBeFalsy();
  await edit.getByRole("button", { name: "取消", exact: true }).click();
  await page.setViewportSize({ width: 1440, height: 1000 });

  await page.getByRole("button", { name: "新增优惠", exact: true }).click();
  await add.locator("#promotion-name input").fill("限时九折优惠码");
  await select(add, "#promotion-kind", "优惠码");
  await add.getByRole("button", { name: "随机生成", exact: true }).click();
  await expect(add.locator("#promotion-code input")).toHaveValue(
    /^[A-Z0-9_-]{3,64}$/,
  );
  await add.locator("#promotion-code input").fill("SAVE10");
  await add.locator("#promotion-value input").fill("10");
  await select(add, "#promotion-scope", "指定商品");
  await select(add, "#promotion-products", first.name);
  await add.locator("#promotion-name input").click();
  await add
    .getByRole("switch", { name: "允许与其他优惠一同使用", exact: true })
    .click();
  function dateText(offset: number) {
    const date = new Date(Date.now() + offset);
    const two = (value: number) => String(value).padStart(2, "0");
    return `${date.getFullYear()}-${two(date.getMonth() + 1)}-${two(date.getDate())} 00:00:00`;
  }
  await add.locator("#promotion-start input").fill(dateText(-86_400_000));
  await add.locator("#promotion-start input").press("Enter");
  await add.locator("#promotion-end input").fill(dateText(86_400_000));
  await add.locator("#promotion-end input").press("Enter");
  const couponID = await saveNew(add);
  const rules = (await (
    await page.request.get(`${base}/api/admin/promotions`)
  ).json()) as Promotion[];
  const coupon = rules.find((rule) => rule.id === couponID)!;
  expect(coupon.starts_at).toBeGreaterThan(0);
  expect(coupon.ends_at).toBeGreaterThan(coupon.starts_at);
  expect(coupon.product_ids).toEqual([first.id]);

  await page.goto(`${base}/products/${first.id}`);
  await expect(page.locator(".product-pricing")).toContainText("80.00 CNY");
  await page.getByRole("button", { name: "下单", exact: true }).click();
  const purchase = page.getByRole("dialog", { name: "确认下单", exact: true });
  await purchase.getByRole("button", { name: "生成", exact: true }).click();
  await expect(purchase.locator(".purchase-total")).toContainText("80.00 CNY");
  await purchase.locator("#purchase-coupon input").fill("INVALID");
  await expect(purchase.getByRole("alert")).toContainText("优惠码不可用");
  await expect(
    purchase.getByRole("button", { name: "前往支付", exact: true }),
  ).toBeDisabled();
  await purchase.locator("#purchase-coupon input").fill("save10");
  await expect(purchase.locator(".purchase-total")).toContainText("72.00 CNY");
  await expect(purchase.locator(".purchase-discounts")).toContainText(
    "常驻八折活动",
  );
  await expect(purchase.locator(".purchase-discounts")).toContainText(
    "限时九折优惠码",
  );
  await page.setViewportSize({ width: 390, height: 900 });
  await expect(
    purchase.getByRole("button", { name: "前往支付", exact: true }),
  ).toBeInViewport();
  await expect(purchase.getByText(/库存/)).toHaveCount(0);
  const { id: _id, used: _used, ...changedCoupon } = coupon;
  const checkoutKey = await purchase.locator("#purchase-key").inputValue();
  expect(
    (
      await page.request.put(`${base}/api/admin/promotions/${couponID}`, {
        headers,
        data: { ...changedCoupon, enabled: false },
      })
    ).ok(),
  ).toBeTruthy();
  await purchase.getByRole("button", { name: "前往支付", exact: true }).click();
  await expect(purchase.getByRole("alert")).toContainText("优惠码不可用");
  await expect(
    purchase.getByRole("button", { name: "前往支付", exact: true }),
  ).toBeDisabled();
  await expect(purchase.locator("#purchase-key")).toHaveValue(checkoutKey);
  await expect(purchase.locator("#purchase-coupon input")).toHaveValue(
    "save10",
  );
  expect(
    (
      await page.request.put(`${base}/api/admin/promotions/${couponID}`, {
        headers,
        data: changedCoupon,
      })
    ).ok(),
  ).toBeTruthy();
  await purchase.getByRole("button", { name: "重新加载", exact: true }).click();
  await expect(purchase.locator(".purchase-total")).toContainText("72.00 CNY");
  expect(
    (
      await page.request.put(`${base}/api/admin/promotions/${couponID}`, {
        headers,
        data: { ...changedCoupon, value: 2000 },
      })
    ).ok(),
  ).toBeTruthy();
  await purchase.getByRole("button", { name: "前往支付", exact: true }).click();
  await expect(purchase.getByRole("alert")).toContainText("重新确认金额");
  await expect(purchase.locator(".purchase-total")).toContainText("64.00 CNY");
  await expect(purchase.locator("#purchase-coupon input")).toHaveValue(
    "save10",
  );
  const { orderID } = await placeTestOrder(page);
  const receipt = `${base}/center/orders/${orderID}`;
  await page.goto(receipt);
  await expect(page.locator(".checkout-amount-block")).toContainText(
    "64.00 CNY",
  );
  expect(
    (await request.post(`${base}/__test/pay/${orderID}`)).ok(),
  ).toBeTruthy();
  await page.reload();
  await expect(page.locator(".delivery-item pre")).toHaveText(
    "PROMOTION-CONTENT",
  );

  // 失效目标能从编辑器移除，不能阻止其他有效商品的优惠维护。
  expect(
    (
      await page.request.delete(`${base}/api/admin/products/${second.id}`, {
        headers,
      })
    ).ok(),
  ).toBeTruthy();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(`${base}/center/promotions`);
  await activity.getByRole("button", { name: "编辑", exact: true }).click();
  await edit
    .locator("#promotion-products .n-tag")
    .filter({ hasText: "已删除商品" })
    .locator(".n-base-close")
    .click();
  await edit.locator("#promotion-name input").fill("修改后的活动名称");
  await edit.getByRole("button", { name: "保存优惠", exact: true }).click();
  await expect(edit).toHaveCount(0);
  const after = (await (
    await page.request.get(`${base}/api/admin/promotions`)
  ).json()) as Promotion[];
  expect(after.find((rule) => rule.id === activityID)!.product_ids).toEqual([
    first.id,
  ]);
  await page
    .getByRole("row")
    .filter({ hasText: "限时九折优惠码" })
    .getByRole("button", { name: "删除", exact: true })
    .click();
  await page
    .locator(".n-dialog")
    .getByRole("button", { name: "删除", exact: true })
    .click();
  await expect(
    page.getByRole("row").filter({ hasText: "限时九折优惠码" }),
  ).toHaveCount(0);
  await page.locator(".locale-switch").click();
  await page
    .locator(".n-base-select-option")
    .getByText("English", { exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Promotions", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("row")
    .filter({ hasText: "修改后的活动名称" })
    .getByRole("button", { name: "Edit", exact: true })
    .click();
  await expect(
    page.getByRole("dialog", { name: "Edit promotion", exact: true }),
  ).toContainText("Allow combining with other discounts");
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.goto(receipt);
  await expect(page.locator(".checkout-amount-block")).toContainText(
    "64.00 CNY",
  );
  await expect(page.locator(".checkout-receipt-meta")).toContainText(
    "常驻八折活动",
  );
  await expect(page.locator(".checkout-receipt-meta")).toContainText(
    "限时九折优惠码",
  );
  await expect(page.locator(".delivery-item pre")).toHaveText(
    "PROMOTION-CONTENT",
  );
  expect(
    (
      await page.request.delete(`${base}/api/admin/promotions/${activityID}`, {
        headers,
      })
    ).ok(),
  ).toBeTruthy();
  expect(
    (
      await page.request.delete(`${base}/api/admin/products/${first.id}`, {
        headers,
      })
    ).ok(),
  ).toBeTruthy();
});
