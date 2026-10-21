import { test, expect } from "./fixture";

test("网页初始化、自动登录与只写私钥", async ({ page, request }) => {
  const fixture = await (
    await request.get(`${process.env.HASHDROP_TEST_URL}/__test/setup-fixture`)
  ).json();
  const base: string = fixture.url;
  await page.goto(`${base}/center`);
  await expect(
    page.getByRole("heading", { name: "初始化 HashDrop" }),
  ).toBeVisible();
  expect(page.url()).toBe(`${base}/setup`);
  await expect(
    page.getByRole("link", { name: "商城", exact: true }),
  ).toHaveCount(0);
  await expect(page.getByLabel("商城公告", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("switch")).toHaveCount(0);
  await page.getByLabel("管理员账号", { exact: true }).fill("store-owner");
  await page.getByLabel("站点名称", { exact: true }).fill("网页部署测试店铺");
  await page.getByLabel("站点地址", { exact: true }).fill(base);
  await page.getByLabel("管理员密码", { exact: true }).fill("1");
  await page.getByLabel("确认密码", { exact: true }).fill("2");
  await page.getByRole("button", { name: "完成初始化", exact: true }).click();
  await expect(
    page.getByText("管理员密码不能为空或超过 256 字符，且两次输入须一致", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.getByLabel("管理员密码", { exact: true })).toHaveValue("1");
  await page.getByLabel("确认密码", { exact: true }).fill("1");
  await page
    .getByLabel("HashPay 地址", { exact: true })
    .fill(fixture.hashpay_url);
  await page.getByLabel("商户 ID", { exact: true }).fill("merchant-test");
  await page
    .getByLabel("商户 RSA 私钥", { exact: true })
    .fill("not-a-private-key");
  await page.getByRole("button", { name: "完成初始化", exact: true }).click();
  await expect(
    page.getByText("HashPay 私钥 PEM 无效", { exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("商户 ID", { exact: true })).toHaveValue(
    "merchant-test",
  );
  await expect(page.getByLabel("商户 RSA 私钥", { exact: true })).toHaveValue(
    "not-a-private-key",
  );
  const siteURL = page.getByLabel("站点地址", { exact: true });
  const callbackURL = page.getByLabel("回调地址：", { exact: true });
  await expect(callbackURL).toHaveAttribute("readonly");
  await siteURL.fill(" https://SHOP.example.com:8443/ ");
  await expect(callbackURL).toHaveValue(
    "https://shop.example.com:8443/api/webhooks/hashpay",
  );
  for (const width of [320, 390, 768, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBeFalsy();
  }
  await siteURL.fill("https://");
  await expect(callbackURL).toHaveValue("");
  await siteURL.fill(base);
  await expect(callbackURL).toHaveValue("");
  await page
    .getByLabel("商户 RSA 私钥", { exact: true })
    .fill(fixture.private_key);
  await page.route("**/api/setup", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 400));
    await route.continue();
  });
  await page.getByRole("button", { name: "完成初始化", exact: true }).click();
  await expect(page.getByLabel("管理员密码", { exact: true })).toBeDisabled();
  await expect(
    page.getByRole("heading", { name: "订单历史", exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(`${base}/center`);
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "订单历史", exact: true }),
  ).toBeVisible();
  await page.unroute("**/api/setup");
  const status = await request.get(`${base}/api/setup/status`);
  expect(await status.json()).toEqual({ initialized: true });
  expect(
    (
      await request.post(`${base}/api/setup`, {
        data: {},
        headers: { Origin: base },
      })
    ).status(),
  ).toBe(409);
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await page.getByRole("button", { name: "管理员登录", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "登录后台" })).toBeVisible();
  await page.getByLabel("管理员账号", { exact: true }).fill("store-owner");
  await page.locator("#password input").fill("1");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "退出登录", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "订单历史", exact: true }),
  ).toBeVisible();
  const cookie = (await page.context().cookies()).find(
    (cookie) => cookie.name === "hashdrop_session",
  );
  expect(cookie).toMatchObject({
    secure: false,
    httpOnly: true,
    sameSite: "Strict",
  });
  await page.getByRole("menuitem", { name: "系统设置", exact: true }).click();
  await expect(page.getByLabel("商户 RSA 私钥", { exact: true })).toHaveValue(
    "",
  );
  const settings = await page.evaluate(async () =>
    (await fetch("/api/admin/settings")).text(),
  );
  expect(settings).not.toContain("PRIVATE KEY");
  expect(settings).not.toContain("admin_password_hash");
  await page.route("**/api/setup/status", (route) => route.abort());
  await page.reload();
  await expect(page.locator("#site-title input")).toHaveValue(
    "网页部署测试店铺",
  );
  await page.unroute("**/api/setup/status");
  await page
    .getByLabel("商户 RSA 私钥", { exact: true })
    .fill(fixture.private_key);
  await page.getByRole("button", { name: "保存设置", exact: true }).click();
  await expect(page.getByText("设置已保存", { exact: true })).toBeVisible();
  await expect(page.getByLabel("商户 RSA 私钥", { exact: true })).toHaveValue(
    "",
  );
  await page.reload();
  await expect(page.locator("#site-title input")).toHaveValue(
    "网页部署测试店铺",
  );
  await page.goto(`${base}/setup`);
  await expect(
    page.getByRole("heading", { name: "订单历史", exact: true }),
  ).toBeVisible();
});
