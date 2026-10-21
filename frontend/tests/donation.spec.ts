import { loginAdmin, test, expect, type Page } from "./fixture";
import type { Donation, DonationSettings } from "../src/api";

const base = () => process.env.HASHDROP_TEST_URL!;
const nickname = (label: string) =>
  `${label}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

async function settingsSession(page: Page) {
  await loginAdmin(page);
  const original = (await (
    await page.request.get(`${base()}/api/admin/donation`)
  ).json()) as DonationSettings;
  const session = await (
    await page.request.get(`${base()}/api/admin/session`)
  ).json();
  return {
    original,
    headers: { Origin: base(), "X-CSRF-Token": session.csrf },
  };
}

async function createDonation(page: Page, name: string) {
  await page.goto(`${base()}/donate`);
  await page.locator("#donate-nickname").fill(name);
  const created = page.waitForResponse(
    (response) =>
      response.url() === `${base()}/api/donations` &&
      response.request().method() === "POST",
  );
  await page
    .getByRole("button", { name: "确认金额并赞赏", exact: true })
    .click();
  const response = await created;
  expect(response.ok(), await response.text()).toBeTruthy();
  const result = (await response.json()) as { donation: Donation };
  await expect(page).toHaveURL(`${base()}/donate/orders/${result.donation.id}`);
  await expect(
    page.getByRole("button", { name: "前往 HashPay 付款", exact: true }),
  ).toBeEnabled({ timeout: 30_000 });
  return result.donation;
}

test("多币种逐项配置、昵称付款与公开选择，停用后仍可查询历史", async ({
  page,
  request,
}) => {
  test.setTimeout(120_000);
  const { original, headers } = await settingsSession(page);
  const privateName = nickname("中文赞赏者");
  try {
    expect(
      (
        await page.request.put(`${base()}/api/admin/donation`, {
          headers,
          data: {
            ...original,
            enabled: true,
            currencies: [
              {
                currency: "CNY",
                amounts: [500],
                min_amount: 100,
                max_amount: 100000,
              },
            ],
          },
        })
      ).ok(),
    ).toBeTruthy();
    await page.goto(`${base()}/center/donation`);
    await page.getByRole("switch", { name: "启用赞赏页" }).check();
    await page
      .getByLabel("页面标题", { exact: true })
      .fill("支持 HashDrop 持续开发");
    await page
      .getByLabel("赞赏说明（Markdown）")
      .fill("## 感谢支持\n你的赞赏将用于项目维护。");
    const groups = page.locator(".donation-currency-group");
    await expect(groups).toHaveCount(1);
    const cny = groups.nth(0);
    await cny.locator(".donation-preset input").nth(0).fill("5");
    await cny.getByRole("button", { name: "添加金额", exact: true }).click();
    await cny.locator(".donation-preset input").nth(1).fill("12.34");
    await cny.getByRole("button", { name: "添加金额", exact: true }).click();
    await cny.locator(".donation-preset input").nth(2).fill("20");
    await cny.getByRole("button", { name: "添加金额", exact: true }).click();
    await cny.locator(".donation-preset input").nth(3).fill("99");
    await cny
      .getByRole("button", { name: "移除金额", exact: true })
      .nth(3)
      .click();
    await page.locator("#donation-min-0").fill("1");
    await page.locator("#donation-max-0").fill("1000");
    for (const [index, currency, amount, min, max] of [
      [1, "USDT", "1.234567", "0.000001", "10"],
      [2, "JPY", "500", "100", "5000"],
    ] as const) {
      await page.getByRole("button", { name: "添加币种", exact: true }).click();
      const group = groups.nth(index);
      await group.getByLabel("赞赏币种", { exact: true }).click();
      await page
        .locator(".n-base-select-option:visible")
        .getByText(currency, { exact: true })
        .click();
      await expect(group.getByLabel("赞赏币种", { exact: true })).toContainText(
        currency,
      );
      await expect(page.locator(".n-base-select-menu:visible")).toHaveCount(0);
      if (!(await group.locator(".donation-preset input").count()))
        await group
          .getByRole("button", { name: "添加金额", exact: true })
          .click();
      while ((await group.locator(".donation-preset input").count()) > 1)
        await group
          .getByRole("button", { name: "移除金额", exact: true })
          .last()
          .click();
      await group.locator(".donation-preset input").nth(0).fill(amount);
      await page.locator(`#donation-min-${index}`).fill(min);
      await page.locator(`#donation-max-${index}`).fill(max);
    }
    await page.getByRole("button", { name: "保存设置", exact: true }).click();
    await expect(
      page.getByText("赞赏页设置已保存", { exact: true }),
    ).toBeVisible();
    const configured = (await (
      await page.request.get(`${base()}/api/admin/donation`)
    ).json()) as DonationSettings;
    expect(configured.currencies).toEqual([
      {
        currency: "CNY",
        amounts: [500, 1234, 2000],
        min_amount: 100,
        max_amount: 100000,
      },
      {
        currency: "USDT",
        amounts: [1234567],
        min_amount: 1,
        max_amount: 10000000,
      },
      { currency: "JPY", amounts: [500], min_amount: 100, max_amount: 5000 },
    ]);
    await page.reload();
    await expect(page.getByLabel("页面标题", { exact: true })).toHaveValue(
      "支持 HashDrop 持续开发",
    );
    await expect(groups).toHaveCount(3);
    for (const [index, amount] of ["5.00", "12.34", "20.00"].entries())
      await expect(
        cny.locator(".donation-preset input").nth(index),
      ).toHaveValue(amount);
    await expect(groups.nth(1).locator(".donation-preset input")).toHaveValue(
      "1.234567",
    );
    await expect(groups.nth(2).locator(".donation-preset input")).toHaveValue(
      "500",
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
      await page.request.get(`${base()}/api/admin/overview`)
    ).json();
    await page.goto(`${base()}/donate`);
    await expect(
      page.getByRole("heading", { name: "支持 HashDrop 持续开发" }),
    ).toBeVisible();
    await expect(
      page.getByRole("heading", { name: "感谢支持" }).first(),
    ).toBeVisible();
    await expect(
      page
        .locator(".donate-page")
        .getByRole("button", { name: /^(生成|复制)$/ }),
    ).toHaveCount(0);
    const visibility = page.getByRole("checkbox", {
      name: "在公开名单显示我的昵称和金额",
      exact: true,
    });
    await expect(visibility).not.toBeChecked();
    await page.locator("#donate-nickname").fill(privateName);
    await page.getByRole("button", { name: "12.34 CNY", exact: true }).click();
    await expect(page.locator("#donate-amount")).toHaveValue("12.34");
    await page.locator("#donate-amount").fill("0.50");
    await page
      .getByRole("button", { name: "确认金额并赞赏", exact: true })
      .click();
    await expect(page.getByRole("alert")).toContainText(
      "赞赏金额不在允许范围内",
    );
    await page.locator("#donate-amount").fill("12.34");
    const created = page.waitForResponse(
      (response) =>
        response.url() === `${base()}/api/donations` &&
        response.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "确认金额并赞赏", exact: true })
      .click();
    const { donation } = (await (await created).json()) as {
      donation: Donation;
    };
    expect(donation.nickname).toBe(privateName);
    expect(donation.show_public).toBe(false);
    await expect(page).toHaveURL(`${base()}/donate/orders/${donation.id}`);
    await expect(page.locator(".donation-receipt")).toContainText(privateName);
    await expect(page.locator("#donation-receipt-link")).toHaveCount(0);
    const payButton = page.getByRole("button", {
      name: "前往 HashPay 付款",
      exact: true,
    });
    await expect(payButton).toBeEnabled({ timeout: 30_000 });
    const detail = (await (
      await page.request.post(`${base()}/api/donations/lookup`, {
        data: { nickname: privateName },
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
      (await request.post(`${base()}/__test/pay-donation/${donation.id}`)).ok(),
    ).toBeTruthy();
    await page.goto(`${base()}/donate/orders/${donation.id}`);
    await expect(
      page.getByRole("heading", { name: "感谢你的赞赏", exact: true }),
    ).toBeVisible();
    await page.reload();
    await expect(page.getByText("已付款", { exact: true })).toBeVisible();
    expect(
      (await request.get(`${base()}/api/donations/${donation.id}`)).status(),
    ).toBe(401);
    expect(
      await (await page.request.get(`${base()}/api/admin/overview`)).json(),
    ).toEqual(shopBefore);

    // 昵称冲突保留用户输入，并让用户主动选择派生推荐。
    await page.goto(`${base()}/donate`);
    await page.locator("#donate-nickname").fill(privateName);
    const conflict = page.waitForResponse(
      (response) =>
        response.url() === `${base()}/api/donations` &&
        response.status() === 409,
    );
    await page
      .getByRole("button", { name: "确认金额并赞赏", exact: true })
      .click();
    const conflictBody = await (await conflict).json();
    expect(conflictBody.code).toBe("donation_nickname_used");
    await expect(page.locator("#donate-nickname")).toHaveValue(privateName);
    const suggestion = page
      .locator(".nickname-suggestions")
      .getByRole("button")
      .first();
    await expect(suggestion).toBeVisible();
    const publicName = (await suggestion.innerText()).trim();
    expect(conflictBody.suggestions).toContain(publicName);
    await suggestion.click();
    await expect(page.locator("#donate-nickname")).toHaveValue(publicName);
    await page.getByLabel("赞赏币种", { exact: true }).click();
    await page
      .locator(".n-base-select-option")
      .getByText("USDT", { exact: true })
      .click();
    await page
      .getByRole("button", { name: "1.234567 USDT", exact: true })
      .click();
    await visibility.check();
    const publicCreated = page.waitForResponse(
      (response) =>
        response.url() === `${base()}/api/donations` &&
        response.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "确认金额并赞赏", exact: true })
      .click();
    const publicDonation = (
      (await (await publicCreated).json()) as { donation: Donation }
    ).donation;
    expect(publicDonation).toMatchObject({
      nickname: publicName,
      show_public: true,
      currency: "USDT",
      amount: 1234567,
    });
    await expect(
      page.getByRole("button", { name: "前往 HashPay 付款", exact: true }),
    ).toBeEnabled({ timeout: 30_000 });
    await page.goto(`${base()}/donate`);
    await expect(
      page.locator(".public-donation").filter({ hasText: publicName }),
    ).toHaveCount(0);
    expect(
      (
        await request.post(`${base()}/__test/pay-donation/${publicDonation.id}`)
      ).ok(),
    ).toBeTruthy();
    await page.reload();
    await expect(
      page.locator(".public-donation").filter({ hasText: publicName }),
    ).toContainText("1.234567 USDT");
    await expect(
      page.locator(".public-donations").getByText(privateName, { exact: true }),
    ).toHaveCount(0);

    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`${base()}/center/donation`);
    await expect(
      page
        .locator(".donation-record")
        .filter({ hasText: donation.id })
        .getByText("已付款", { exact: true }),
    ).toBeVisible();
    await page.getByRole("switch", { name: "启用赞赏页" }).uncheck();
    await page.getByRole("button", { name: "保存设置", exact: true }).click();
    await expect(
      page.getByText("赞赏页设置已保存", { exact: true }),
    ).toBeVisible();
    await page.goto(`${base()}/donate`);
    await expect(
      page.getByText("赞赏页暂未开启。", { exact: true }),
    ).toBeVisible();
    await page.goto(`${base()}/center/donations`);
    await page.locator("#donation-query-nickname").fill(privateName);
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "感谢你的赞赏", exact: true }),
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
    expect(
      (
        await page.request.put(`${base()}/api/admin/donation`, {
          headers,
          data: original,
        })
      ).ok(),
    ).toBeTruthy();
  }
});

test("赞赏创建响应丢失后重试沿用请求标识，只保留同一笔记录", async ({
  page,
}) => {
  const { original, headers } = await settingsSession(page);
  try {
    expect(
      (
        await page.request.put(`${base()}/api/admin/donation`, {
          headers,
          data: {
            ...original,
            enabled: true,
            currencies: [
              {
                currency: "CNY",
                amounts: [500],
                min_amount: 100,
                max_amount: 100000,
              },
            ],
          },
        })
      ).ok(),
    ).toBeTruthy();
    const name = nickname("断线重试");
    await page.goto(`${base()}/donate`);
    await page.locator("#donate-nickname").fill(name);
    const submissions: {
      request_id: string;
      nickname: string;
      amount: number;
      currency: string;
      show_public: boolean;
    }[] = [];
    let firstDonation: Donation | undefined;
    await page.route(`${base()}/api/donations`, async (route) => {
      if (route.request().method() !== "POST") return route.continue();
      submissions.push(route.request().postDataJSON());
      if (submissions.length !== 1) return route.continue();
      // 真实服务器已落单，只丢弃返回给浏览器的响应。
      const response = await route.fetch();
      expect(response.ok(), await response.text()).toBeTruthy();
      firstDonation = ((await response.json()) as { donation: Donation })
        .donation;
      await route.abort("failed");
    });
    await page
      .getByRole("button", { name: "确认金额并赞赏", exact: true })
      .click();
    await expect(
      page.getByRole("alert").filter({ hasText: "网络请求失败" }),
    ).toBeVisible();
    await expect(page.locator("#donate-nickname")).toHaveValue(name);
    await page.reload();
    await expect(page.locator("#donate-nickname")).toHaveValue(name);
    await expect(page.locator("#donate-nickname")).toBeDisabled();
    await expect(page.locator("#donate-amount")).toBeDisabled();
    const retried = page.waitForResponse(
      (response) =>
        response.url() === `${base()}/api/donations` &&
        response.request().method() === "POST",
    );
    await page
      .getByRole("button", { name: "确认金额并赞赏", exact: true })
      .click();
    const response = await retried;
    expect(response.ok(), await response.text()).toBeTruthy();
    const recovered = ((await response.json()) as { donation: Donation })
      .donation;
    expect(firstDonation).toBeDefined();
    expect(recovered.id).toBe(firstDonation!.id);
    expect(submissions).toHaveLength(2);
    expect(submissions[0]!.request_id).toBeTruthy();
    expect(submissions[1]).toEqual(submissions[0]);
    await expect(page).toHaveURL(`${base()}/donate/orders/${recovered.id}`);
    await page.goto(`${base()}/center/donations`);
    await expect(page.locator(".donation-history-row")).toHaveCount(1);
    await expect(page.locator(".donation-history-row")).toContainText(
      recovered.id,
    );
    const records = await (
      await page.request.get(`${base()}/api/admin/donations?page=1`)
    ).json();
    expect(
      records.items.filter((item: Donation) => item.nickname === name),
    ).toHaveLength(1);
  } finally {
    expect(
      (
        await page.request.put(`${base()}/api/admin/donation`, {
          headers,
          data: original,
        })
      ).ok(),
    ).toBeTruthy();
  }
});

test("创建结果未知后按昵称找回记录，返回赞赏页可以填写新的昵称", async ({
  page,
}) => {
  const { original, headers } = await settingsSession(page);
  try {
    expect(
      (
        await page.request.put(`${base()}/api/admin/donation`, {
          headers,
          data: {
            ...original,
            enabled: true,
            currencies: [
              {
                currency: "CNY",
                amounts: [500],
                min_amount: 100,
                max_amount: 100000,
              },
            ],
          },
        })
      ).ok(),
    ).toBeTruthy();
    const name = nickname("昵称找回未知结果");
    let createdID = "";
    await page.route(`${base()}/api/donations`, async (route) => {
      if (route.request().method() !== "POST" || createdID)
        return route.continue();
      const response = await route.fetch();
      expect(response.ok(), await response.text()).toBeTruthy();
      createdID = ((await response.json()) as { donation: Donation }).donation
        .id;
      await route.abort("failed");
    });
    await page.goto(`${base()}/donate`);
    await page.locator("#donate-nickname").fill(name);
    await page
      .getByRole("button", { name: "确认金额并赞赏", exact: true })
      .click();
    await expect(
      page.getByRole("alert").filter({ hasText: "网络请求失败" }),
    ).toBeVisible();
    await expect(page.locator("#donate-nickname")).toBeDisabled();
    await page.goto(`${base()}/center/donations`);
    await page.locator("#donation-query-nickname").fill(name);
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect(page).toHaveURL(`${base()}/center/donations/${createdID}`);
    await expect(page.locator(".donation-receipt")).toContainText(name);
    // 找回同一请求的结果后，待确认状态应已解除，不能锁住后续赞赏。
    await page.goto(`${base()}/donate`);
    await expect(page.locator("#donate-nickname")).toBeEditable();
    await expect(page.locator("#donate-nickname")).toHaveValue("");
    const nextName = nickname("后续赞赏");
    await page.locator("#donate-nickname").fill(nextName);
    await expect(page.locator("#donate-nickname")).toHaveValue(nextName);
  } finally {
    expect(
      (
        await page.request.put(`${base()}/api/admin/donation`, {
          headers,
          data: original,
        })
      ).ok(),
    ).toBeTruthy();
  }
});

test("320px 长标题与受限存储下仍可用中文昵称付款和查询", async ({
  page,
  request,
}) => {
  const { original, headers } = await settingsSession(page);
  const title = "A".repeat(120);
  const name = nickname("浏览器未保存");
  try {
    expect(
      (
        await page.request.put(`${base()}/api/admin/donation`, {
          headers,
          data: {
            ...original,
            title,
            enabled: true,
            currencies: [
              {
                currency: "CNY",
                amounts: [500],
                min_amount: 100,
                max_amount: 100000,
              },
            ],
          },
        })
      ).ok(),
    ).toBeTruthy();
    await page.setViewportSize({ width: 320, height: 740 });
    await page.addInitScript(() => {
      for (const name of ["localStorage", "sessionStorage"])
        Object.defineProperty(window, name, {
          get() {
            throw new DOMException("Storage blocked", "SecurityError");
          },
        });
    });
    const donation = await createDonation(page, name);
    await expect(page.locator(".donation-receipt")).toContainText(name);
    await expect(page.locator("#donation-receipt-link")).toHaveCount(0);
    await expect(
      page.getByRole("checkbox", { name: "我已保存完整查询链接" }),
    ).toHaveCount(0);
    expect(
      (await request.post(`${base()}/__test/pay-donation/${donation.id}`)).ok(),
    ).toBeTruthy();
    await page.goto(`${base()}/center/donations`);
    await page.locator("#donation-query-nickname").fill(name);
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await expect(
      page.getByRole("heading", { name: "感谢你的赞赏", exact: true }),
    ).toBeVisible();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBeFalsy();
    await page.goto(`${base()}/donate`);
    const heading = page.getByRole("heading", { name: title, exact: true });
    await expect(heading).toBeVisible();
    expect(
      await heading.evaluate(
        (element) => element.scrollWidth <= element.clientWidth + 1,
      ),
    ).toBeTruthy();
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBeFalsy();
  } finally {
    expect(
      (
        await page.request.put(`${base()}/api/admin/donation`, {
          headers,
          data: original,
        })
      ).ok(),
    ).toBeTruthy();
  }
});
