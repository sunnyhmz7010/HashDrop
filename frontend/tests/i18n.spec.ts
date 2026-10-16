import { test, expect, type Page } from "./fixture";
import type { Product } from "../src/api";
import { placeTestOrder } from "./checkout";

test.use({ locale: "en-US" });

async function switchLanguage(page: Page, name: string) {
  await page.locator(".locale-switch").click();
  await page
    .locator(".n-base-select-option")
    .getByText(name, { exact: true })
    .click();
}

test("中英文切换、统一顶栏与管理入口", async ({ page, request }) => {
  const base = process.env.HASHDROP_TEST_URL!;
  const fixture = await (
    await request.get(`${base}/__test/setup-fixture`)
  ).json();
  const setup = await page.context().newPage();
  await setup.setViewportSize({ width: 320, height: 900 });
  await setup.goto(fixture.url);
  await expect(
    setup.getByRole("heading", { name: "Initialize HashDrop" }),
  ).toBeVisible();
  await setup.getByLabel("Site name", { exact: true }).fill("Draft title");
  await switchLanguage(setup, "中文");
  await expect(setup.getByLabel("站点名称", { exact: true })).toHaveValue(
    "Draft title",
  );
  await switchLanguage(setup, "English");
  expect(
    await setup.evaluate(
      () => document.documentElement.scrollWidth > innerWidth + 1,
    ),
  ).toBeFalsy();
  await setup.close();
  await page.goto(base);
  await expect(
    page.getByRole("link", { name: "Details" }).first(),
  ).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  await expect(
    page.locator(".topbar").getByRole("link", { name: "Query center" }),
  ).toBeVisible();
  const adminEntry = page
    .locator(".topbar")
    .getByRole("button", { name: "Query center", exact: true });
  await expect(adminEntry).toBeVisible();
  await adminEntry.click();
  await expect(page.getByRole("heading", { name: "Order history", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Admin sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Admin sign in", exact: true }),
  ).toBeVisible();
  await page.locator(".topbar .brand").click();
  const publicBrand = (await page.locator(".topbar .brand").boundingBox())!;

  // 真实创建固定文本订单，切换语言不能改变领取凭证或业务内容。
  const products: Product[] = await (
    await request.get(`${base}/api/products`)
  ).json();
  const staticProduct = products.find((product) => product.kind === "static")!;
  await page
    .locator(".product-card")
    .filter({ has: page.locator(`a[href="/products/${staticProduct.id}"]`) })
    .getByRole("link", { name: "Details" })
    .click();
  await expect(page.locator(".product-summary")).toBeVisible();
  await expect(
    page
      .locator(".topbar")
      .getByRole("button", { name: "Query center", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Place order", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "Confirm order" }),
  ).toBeVisible();
  const { key: token, orderID } = await placeTestOrder(page);
  await page.goto(`${base}/center/query`);
  await expect(
    page.getByRole("button", { name: "Pay with HashPay" }),
  ).toBeVisible({ timeout: 15_000 });
  const receipt = page.url();
  await switchLanguage(page, "中文");
  await expect(
    page.getByRole("button", { name: "前往 HashPay 付款" }),
  ).toBeVisible();
  expect(page.url()).toBe(receipt);
  await switchLanguage(page, "English");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  expect(page.url()).toBe(receipt);
  expect(
    (await request.post(`${base}/__test/pay/${orderID}`)).ok(),
  ).toBeTruthy();
  await page
    .getByRole("button", { name: "Refresh status", exact: true })
    .click();
  await expect(
    page.getByText("Payment confirmed", { exact: true }),
  ).toBeVisible();
  const order = await (
    await request.post(`${base}/api/orders/lookup`, {
      data: { key: token },
    })
  ).json();
  await expect(page.locator(".delivery-item pre")).toHaveText(
    order.deliveries[0].content,
  );

  await page.goto(`${base}/center/login`);
  await page.locator("#password input").fill("wrong-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByText("Incorrect username or password", {
      exact: true,
    }),
  ).toBeVisible();
  await switchLanguage(page, "中文");
  await expect(
    page.getByText("账号或密码不正确", { exact: true }),
  ).toBeVisible();
  await expect(page.locator("#password input")).toHaveValue("wrong-password");
  await switchLanguage(page, "English");
  await page.locator("#password input").fill("test-password-only");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Overview", exact: true }),
  ).toBeVisible();
  await expect(page.locator(".topbar a")).toHaveCount(1);
  await expect(
    page.locator(".topbar").getByRole("button", { name: "Sign out" }),
  ).toBeVisible();
  await expect(
    page
      .locator(".workspace-release")
      .getByRole("button", { name: "Sign out" }),
  ).toHaveCount(0);
  const adminBrand = (await page.locator(".topbar .brand").boundingBox())!;
  const adminLanguage = (await page.locator(".locale-switch").boundingBox())!;
  const signOut = (await page
    .locator(".topbar")
    .getByRole("button", { name: "Sign out" })
    .boundingBox())!;
  expect(adminBrand.x).toBe(publicBrand.x);
  expect(adminBrand.y).toBe(publicBrand.y);
  expect(signOut.x).toBeGreaterThanOrEqual(
    adminLanguage.x + adminLanguage.width,
  );

  await page
    .getByRole("menuitem", { name: "System settings", exact: true })
    .click();
  await page.locator("#site-title input").fill("Unsaved title");
  await switchLanguage(page, "中文");
  await expect(
    page.getByRole("heading", { name: "系统设置", exact: true }),
  ).toBeVisible();
  await expect(page.locator("#site-title input")).toHaveValue("Unsaved title");
  await switchLanguage(page, "English");
  await page.getByRole("menuitem", { name: "Products", exact: true }).click();
  await page.getByRole("button", { name: "Add product", exact: true }).click();
  await expect(page.locator("#product-inventory")).toBeVisible();
  await expect(page.locator("#static-content textarea")).toHaveCount(0);
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.getByRole("menuitem", { name: "Inventory", exact: true }).click();
  await page
    .getByRole("button", { name: "Add inventory pool", exact: true })
    .click();
  await page.locator("#inventory-kind").click();
  await expect(page.locator(".n-base-select-option:visible")).toHaveCount(3);
  await expect(
    page
      .locator(".n-base-select-option")
      .getByText("License key", { exact: true }),
  ).toBeVisible();
  await page
    .locator(".n-base-select-option")
    .getByText("Static text", { exact: true })
    .click();
  await expect(page.locator("#static-content textarea")).toBeVisible();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.locator(".topbar .brand").click();
  await expect(page).toHaveURL(`${base}/`);
  await page.goto(`${base}/center/query`);
  await page
    .getByRole("button", { name: "Find another order", exact: true })
    .click();
  await page.locator("#claim-key").fill("test");
  await page.getByRole("button", { name: "View order", exact: true }).click();
  await expect(
    page.getByText("Invalid order or claim credential", {
      exact: true,
    }),
  ).toBeVisible();
  await switchLanguage(page, "中文");
  await expect(page.locator("#claim-key")).toHaveValue("test");
  await page.getByRole("button", { name: "查看订单", exact: true }).click();
  await expect(page.getByText("订单或凭证无效", { exact: true })).toBeVisible();
  await switchLanguage(page, "English");

  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 450 });
    await page.goto(base);
    await expect(
      page.getByRole("button", { name: "Query center", exact: true }),
    ).toBeInViewport();
    const shopBrand = (await page.locator(".topbar .brand").boundingBox())!;
    await page.goto(`${base}/center/orders`);
    await expect(
      page.getByRole("heading", { name: "Orders", exact: true }),
    ).toBeVisible();
    const managementBrand = (await page
      .locator(".topbar .brand")
      .boundingBox())!;
    expect(managementBrand.x).toBe(shopBrand.x);
    await expect(
      page
        .locator(".topbar")
        .getByRole("button", { name: "Sign out", exact: true }),
    ).toBeInViewport();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBeFalsy();
    await page.getByRole("button", { name: "Open admin menu" }).click();
    await expect
      .poll(async () => (await page.locator(".n-drawer").boundingBox())?.x)
      .toBe(0);
    await expect(
      page
        .locator(".n-drawer")
        .getByRole("button", { name: "Sign out", exact: true }),
    ).toHaveCount(0);
    await page.keyboard.press("Escape");
    await expect(page.locator(".n-drawer")).toBeHidden();
    if (width === 390) {
      await page.getByRole("button", { name: "Sign out", exact: true }).click();
      await expect(
        page.getByRole("heading", { name: "Admin sign in" }),
      ).toBeVisible();
      expect(
        (await page.request.get(`${base}/api/admin/session`)).status(),
      ).toBe(401);
    }
  }
  await page.goto(`${base}/center/orders`);
  await expect(
    page.getByRole("heading", { name: "Order history" }),
  ).toBeVisible();

  const blocked = await page.context().newPage();
  await blocked.addInitScript(() => {
    Storage.prototype.getItem = Storage.prototype.setItem = () => {
      throw new Error("Storage unavailable");
    };
  });
  await blocked.goto(base);
  await expect(
    blocked.getByRole("link", { name: "Details" }).first(),
  ).toBeVisible();
  await switchLanguage(blocked, "中文");
  await expect(
    blocked.getByRole("link", { name: "详情" }).first(),
  ).toBeVisible();
  await blocked.close();
});
