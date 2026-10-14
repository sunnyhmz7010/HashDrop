import { test, expect } from "./fixture";

test("网页初始化、失败重填、HTTP 登录与只写私钥", async ({ page, request }) => {
  const fixture = await (
    await request.get(`${process.env.TINYFAKA_TEST_URL}/__test/setup-fixture`)
  ).json();
  const base: string = fixture.url;
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(`${base}/admin`);
  await expect(
    page.getByRole("heading", { name: "初始化 TinyFaka" }),
  ).toBeVisible();
  expect(page.url()).toBe(`${base}/setup`);
  await expect(
    page.getByRole("link", { name: "商城", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByLabel("一次性初始化凭证", { exact: true }),
  ).toHaveCount(0);
  await expect(page.getByLabel("商城公告", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("switch")).toHaveCount(0);
  await page.getByLabel("管理员账号", { exact: true }).fill("store-owner");
  await page.getByLabel("站点名称", { exact: true }).fill("网页部署测试店铺");
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
  for (const width of [320, 390, 768, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBeFalsy();
    if (width === 390 || width === 1440)
      await page.screenshot({
        path: `../.local/setup-${width}.png`,
        fullPage: true,
      });
  }
  await page
    .getByLabel("商户 RSA 私钥", { exact: true })
    .fill(fixture.private_key);
  await page.route("**/api/setup", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 400));
    await route.continue();
  });
  await page.getByRole("button", { name: "完成初始化", exact: true }).click();
  await expect(page.getByLabel("管理员密码", { exact: true })).toBeDisabled();
  await expect(page.getByRole("heading", { name: "登录后台" })).toBeVisible();
  await expect(
    page.getByLabel("动态验证码（已启用时填写）", { exact: true }),
  ).toHaveCount(0);
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
  await page.getByLabel("管理员账号", { exact: true }).fill("store-owner");
  await page.locator("#password input").fill("1");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "概览", exact: true }),
  ).toBeVisible();
  const cookie = (await page.context().cookies()).find(
    (cookie) => cookie.name === "tinyfaka_session",
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
  await expect(
    page.getByText("商户私钥已保存；请通过小额订单验证接入。", { exact: true }),
  ).toBeVisible();
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
  await expect(page.getByRole("heading", { name: "登录后台" })).toBeVisible();
  expect(errors).toEqual([]);
});
