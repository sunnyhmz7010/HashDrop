import { loginAdmin, test, expect } from "./fixture";
import type { Donation, DonationSettings } from "../src/api";

test("赞赏页配置、独立付款查询、手机菜单与停用后的历史记录", async ({
  page,
  request,
}) => {
  test.setTimeout(90_000);
  const base = process.env.HASHDROP_TEST_URL!;
  await loginAdmin(page);
  await page.getByRole("menuitem", { name: "赞赏页", exact: true }).click();
  await expect(page).toHaveURL(`${base}/admin/donation`);
  await expect(page.getByLabel("页面标题", { exact: true })).toBeVisible();
  const original = (await (
    await page.request.get(`${base}/api/admin/donation`)
  ).json()) as DonationSettings;
  const session = await (
    await page.request.get(`${base}/api/admin/session`)
  ).json();
  const headers = { Origin: base, "X-CSRF-Token": session.csrf };
  try {
    await page.getByRole("switch", { name: "启用赞赏页" }).check();
    await page
      .getByLabel("页面标题", { exact: true })
      .fill("支持 HashDrop 持续开发");
    await page
      .getByLabel("赞赏说明（Markdown）")
      .fill("## 感谢支持\n你的赞赏将用于项目维护。");
    await page.locator("#donation-presets").fill("5, 12.34, 20");
    await page.locator("#donation-min").fill("1");
    await page.locator("#donation-max").fill("1000");
    await page.getByRole("button", { name: "保存设置", exact: true }).click();
    await expect(
      page.getByText("赞赏页设置已保存", { exact: true }),
    ).toBeVisible();
    await page.reload();
    await expect(page.getByLabel("页面标题", { exact: true })).toHaveValue(
      "支持 HashDrop 持续开发",
    );
    await page.setViewportSize({ width: 390, height: 844 });
    await page.getByRole("button", { name: "打开管理菜单" }).click();
    await page
      .locator(".workspace-nav--drawer")
      .getByRole("menuitem", { name: "赞赏页", exact: true })
      .click();
    await expect(page.locator(".workspace-nav--drawer")).not.toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBeFalsy();

    const shopBefore = await (
      await page.request.get(`${base}/api/admin/overview`)
    ).json();
    await page.goto(`${base}/donate`);
    await expect(
      page.getByRole("heading", { name: "支持 HashDrop 持续开发" }),
    ).toBeVisible();
    await expect(page.getByRole("heading", { name: "感谢支持" })).toBeVisible();
    await page.getByRole("button", { name: "12.34 CNY", exact: true }).click();
    await expect(page.locator("#donate-amount")).toHaveValue("12.34");
    await page.locator("#donate-amount").fill("0.50");
    await page.getByRole("button", { name: "确认金额并赞赏" }).click();
    await expect(page.getByRole("alert")).toContainText(
      "赞赏金额不在允许范围内",
    );
    await page.locator("#donate-amount").fill("12.34");
    const created = page.waitForResponse(
      (response) =>
        response.url() === `${base}/api/donations` &&
        response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "确认金额并赞赏" }).click();
    const result = (await (await created).json()) as {
      donation: Donation;
      token: string;
    };
    const id = result.donation.id;
    await expect(page).toHaveURL(new RegExp(`/donate/orders/${id}#token=`));
    await expect(page.getByText("12.34 CNY", { exact: true })).toBeVisible();
    const payButton = page.getByRole("button", { name: "前往 HashPay 付款" });
    await expect(payButton).toBeVisible({ timeout: 30_000 });
    const detail = (await (
      await page.request.get(`${base}/api/donations/${id}`, {
        headers: { Authorization: `Bearer ${result.token}` },
      })
    ).json()) as Donation;
    await page.route(detail.checkout_url, (route) =>
      route.fulfill({
        contentType: "text/html",
        body: "<html><body>HashPay test checkout</body></html>",
      }),
    );
    await payButton.click();
    await expect(page).toHaveURL(detail.checkout_url);
    expect(
      (await request.post(`${base}/__test/pay-donation/${id}`)).ok(),
    ).toBeTruthy();
    await page.goto(`${base}/donate/orders/${id}`);
    await expect(
      page.getByRole("heading", { name: "感谢你的赞赏" }),
    ).toBeVisible();
    await expect(page.getByText("已付款", { exact: true })).toBeVisible();
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "感谢你的赞赏" }),
    ).toBeVisible();
    expect((await request.get(`${base}/api/donations/${id}`)).status()).toBe(
      401,
    );
    const shopAfter = await (
      await page.request.get(`${base}/api/admin/overview`)
    ).json();
    expect(shopAfter).toEqual(shopBefore);

    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`${base}/admin/donation`);
    await expect(
      page
        .locator(".donation-record")
        .filter({ hasText: id })
        .getByText("已付款", { exact: true }),
    ).toBeVisible();
    await page.getByRole("switch", { name: "启用赞赏页" }).uncheck();
    await page.getByRole("button", { name: "保存设置", exact: true }).click();
    await expect(
      page.getByText("赞赏页设置已保存", { exact: true }),
    ).toBeVisible();
    await page.goto(`${base}/donate`);
    await expect(
      page.getByText("赞赏页暂未开启。", { exact: true }),
    ).toBeVisible();
    await page.goto(`${base}/donate/orders/${id}#token=${result.token}`);
    await expect(
      page.getByRole("heading", { name: "感谢你的赞赏" }),
    ).toBeVisible();
    await page.locator(".locale-switch").click();
    await page
      .locator(".n-base-select-option")
      .getByText("English", { exact: true })
      .click();
    await expect(
      page.getByRole("heading", {
        name: "Thank you for your support",
        exact: true,
      }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBeFalsy();
  } finally {
    const restored = await page.request.put(`${base}/api/admin/donation`, {
      headers,
      data: original,
    });
    expect(restored.ok()).toBeTruthy();
  }
});

test("窄屏长标题完整展示，凭证无法存储时先保存查询链接", async ({
  page,
  request,
}) => {
  const base = process.env.HASHDROP_TEST_URL!;
  await page.setViewportSize({ width: 320, height: 740 });
  await loginAdmin(page);
  await expect(page).toHaveURL(`${base}/admin`);
  const session = await (
    await page.request.get(`${base}/api/admin/session`)
  ).json();
  const headers = { Origin: base, "X-CSRF-Token": session.csrf };
  const original = (await (
    await page.request.get(`${base}/api/admin/donation`)
  ).json()) as DonationSettings;
  const title = "A".repeat(120);
  let donationID = "";
  try {
    const saved = await page.request.put(`${base}/api/admin/donation`, {
      headers,
      data: {
        ...original,
        title,
        enabled: true,
        currency: "CNY",
        amounts: [500],
        min_amount: 100,
        max_amount: 100000,
      },
    });
    expect(saved.ok()).toBeTruthy();
    await page.addInitScript(() => {
      Object.defineProperty(window, "sessionStorage", {
        get() {
          throw new DOMException("Storage blocked", "SecurityError");
        },
      });
    });
    await page.goto(`${base}/donate`);
    const heading = page.getByRole("heading", { name: title, exact: true });
    await expect(heading).toBeVisible();
    expect(
      await heading.evaluate(
        (element) => element.scrollWidth <= element.clientWidth + 1,
      ),
    ).toBeTruthy();
    const created = page.waitForResponse(
      (response) =>
        response.url() === `${base}/api/donations` &&
        response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "确认金额并赞赏" }).click();
    const result = (await (await created).json()) as {
      donation: Donation;
      token: string;
    };
    donationID = result.donation.id;
    const pay = page.getByRole("button", { name: "前往 HashPay 付款" });
    await expect(pay).toBeVisible({ timeout: 30_000 });
    await expect(pay).toBeDisabled();
    const savedLink = await page
      .getByLabel("赞赏查询链接", { exact: true })
      .inputValue();
    expect(savedLink).toContain(`#token=${result.token}`);
    await page.getByRole("checkbox", { name: "我已保存完整查询链接" }).check();
    await expect(pay).toBeEnabled();
    expect(
      await page
        .locator(".donation-receipt p")
        .first()
        .evaluate((element) => element.scrollWidth <= element.clientWidth + 1),
    ).toBeTruthy();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBeFalsy();
    await page.goto(`${base}/donate/orders/${donationID}`);
    await expect(page.getByRole("alert")).toContainText("缺少查询凭证");
    await expect(
      page.getByRole("button", { name: "前往 HashPay 付款" }),
    ).not.toBeVisible();
  } finally {
    if (donationID)
      expect(
        (await request.post(`${base}/__test/pay-donation/${donationID}`)).ok(),
      ).toBeTruthy();
    expect(
      (
        await page.request.put(`${base}/api/admin/donation`, {
          headers,
          data: original,
        })
      ).ok(),
    ).toBeTruthy();
  }
});
