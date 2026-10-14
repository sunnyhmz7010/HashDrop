import { test, expect, type Page } from "@playwright/test";

test.use({ locale: "en-US" });

async function switchLanguage(page: Page, name: string) {
  await page.locator(".locale-switch").click();
  await page
    .locator(".n-base-select-option")
    .getByText(name, { exact: true })
    .click();
}

test("中英文切换、统一顶栏与侧栏退出", async ({ page, request }) => {
  const base = process.env.TINYFAKA_TEST_URL!;
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const fixture = await (
    await request.get(`${base}/__test/setup-fixture`)
  ).json();
  const setup = await page.context().newPage();
  await setup.setViewportSize({ width: 320, height: 900 });
  await setup.goto(fixture.url);
  await expect(
    setup.getByRole("heading", { name: "Initialize TinyFaka" }),
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
  await setup.screenshot({
    path: "../.local/setup-en-mobile.png",
    fullPage: true,
  });
  await setup.close();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(base);
  await expect(
    page.getByRole("button", { name: "View product" }).first(),
  ).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  await expect(page.locator(".topbar a")).toHaveCount(1);
  const publicBrand = (await page.locator(".topbar .brand").boundingBox())!;
  const publicLanguage = (await page.locator(".locale-switch").boundingBox())!;
  await page.screenshot({ path: "../.local/shop-en.png", fullPage: true });

  // 真实创建固定文本订单，切换语言不能改变领取凭证或业务内容。
  await page
    .locator(".product-card")
    .filter({ hasText: "Static text" })
    .getByRole("button", { name: "View product" })
    .click();
  await expect(
    page.getByRole("heading", { name: "Confirm purchase" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Create order", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Pay with HashPay" }),
  ).toBeVisible({ timeout: 15_000 });
  const receipt = page.url();
  const orderID = new URL(receipt).pathname.split("/").pop()!;
  const token = new URLSearchParams(new URL(receipt).hash.slice(1)).get(
    "token",
  )!;
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
    await request.get(`${base}/api/orders/${orderID}`, {
      headers: { Authorization: `Bearer ${token}` },
    })
  ).json();
  await expect(page.locator(".delivery-item pre")).toHaveText(
    order.deliveries[0].content,
  );
  await page.screenshot({ path: "../.local/receipt-en.png", fullPage: true });

  await page.goto(`${base}/admin/login`);
  await page.locator("#password input").fill("wrong-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(
    page.getByText("Incorrect username, password or one-time code", {
      exact: true,
    }),
  ).toBeVisible();
  await switchLanguage(page, "中文");
  await expect(
    page.getByText("账号、密码或动态验证码不正确", { exact: true }),
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
  ).toHaveCount(0);
  await expect(
    page
      .locator(".workspace-release")
      .getByRole("button", { name: "Sign out" }),
  ).toBeVisible();
  const adminBrand = (await page.locator(".topbar .brand").boundingBox())!;
  const adminLanguage = (await page.locator(".locale-switch").boundingBox())!;
  expect(adminBrand.x).toBe(publicBrand.x);
  expect(adminBrand.y).toBe(publicBrand.y);
  expect(adminLanguage.x).toBe(publicLanguage.x);
  await page.screenshot({ path: "../.local/admin-en.png", fullPage: true });

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
  await page
    .getByRole("menuitem", { name: "Products and stock", exact: true })
    .click();
  await page.getByRole("button", { name: "Add product", exact: true }).click();
  await page.locator("#product-kind").click();
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
  await page.goto(`${base}/claim`);
  await page.locator("#claim-link input").fill("test");
  await page.getByRole("button", { name: "View order", exact: true }).click();
  await expect(
    page.getByText("Enter a complete claim link from this site", {
      exact: true,
    }),
  ).toBeVisible();
  await switchLanguage(page, "中文");
  await expect(page.locator("#claim-link input")).toHaveValue("test");
  await page.getByRole("button", { name: "查看订单", exact: true }).click();
  await expect(
    page.getByText("请填写本站完整领取链接", { exact: true }),
  ).toBeVisible();
  await switchLanguage(page, "English");

  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 450 });
    await page.goto(base);
    const shopBrand = (await page.locator(".topbar .brand").boundingBox())!;
    await page.goto(`${base}/admin/orders`);
    await expect(
      page.getByRole("heading", { name: "Order management" }),
    ).toBeVisible();
    const managementBrand = (await page
      .locator(".topbar .brand")
      .boundingBox())!;
    expect(managementBrand.x).toBe(shopBrand.x);
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
      page.getByRole("button", { name: "Sign out", exact: true }),
    ).toBeInViewport();
    if (width === 390) {
      await page.screenshot({
        path: "../.local/admin-menu-en-mobile.png",
        animations: "disabled",
      });
      await page.getByRole("button", { name: "Sign out", exact: true }).click();
      await expect(
        page.getByRole("heading", { name: "Admin sign in" }),
      ).toBeVisible();
      expect(
        (await page.request.get(`${base}/api/admin/session`)).status(),
      ).toBe(401);
    } else await page.keyboard.press("Escape");
  }
  await page.goto(`${base}/admin/orders`);
  await expect(
    page.getByRole("heading", { name: "Admin sign in" }),
  ).toBeVisible();

  const blocked = await page.context().newPage();
  await blocked.addInitScript(() => {
    Storage.prototype.getItem = Storage.prototype.setItem = () => {
      throw new Error("Storage unavailable");
    };
  });
  await blocked.goto(base);
  await expect(
    blocked.getByRole("button", { name: "View product" }).first(),
  ).toBeVisible();
  await switchLanguage(blocked, "中文");
  await expect(
    blocked.getByRole("button", { name: "查看商品" }).first(),
  ).toBeVisible();
  await blocked.close();
  expect(errors).toEqual([]);
});
