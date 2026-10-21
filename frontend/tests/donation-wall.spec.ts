import { loginAdmin, test, expect, type Page } from "./fixture";
import type { Donation, DonationList, DonationSettings } from "../src/api";

const base = () => process.env.HASHDROP_TEST_URL!;
const uniqueName = () =>
  `再次支持-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

async function configure(page: Page) {
  await loginAdmin(page);
  const original = (await (
    await page.request.get(`${base()}/api/admin/donation`)
  ).json()) as DonationSettings;
  const session = await (
    await page.request.get(`${base()}/api/admin/session`)
  ).json();
  const headers = { Origin: base(), "X-CSRF-Token": session.csrf };
  const response = await page.request.put(`${base()}/api/admin/donation`, {
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
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  return async () => {
    expect(
      (
        await page.request.put(`${base()}/api/admin/donation`, {
          headers,
          data: original,
        })
      ).ok(),
    ).toBeTruthy();
  };
}

async function donate(page: Page, nickname: string) {
  await page.goto(`${base()}/donate`);
  await page.locator("#donate-nickname").fill(nickname);
  const response = page.waitForResponse(
    (response) =>
      response.url() === `${base()}/api/donations` &&
      response.request().method() === "POST",
  );
  await page
    .getByRole("button", { name: "确认金额并赞赏", exact: true })
    .click();
  return response;
}

test("同昵称再次赞赏需确认，概览卡片聚合完整历史并可核验，昵称查询找回全部记录", async ({
  page,
  browser,
  request,
}) => {
  test.setTimeout(120_000);
  const restore = await configure(page);
  const name = uniqueName();
  try {
    const firstResponse = await donate(page, name);
    expect(firstResponse.ok()).toBeTruthy();
    const first = ((await firstResponse.json()) as { donation: Donation })
      .donation;
    await expect(page).toHaveURL(`${base()}/donate/orders/${first.id}`);
    await expect(
      page.getByRole("button", { name: "前往 HashPay 付款", exact: true }),
    ).toBeEnabled({ timeout: 30_000 });
    expect(
      (await request.post(`${base()}/__test/pay-donation/${first.id}`)).ok(),
    ).toBeTruthy();

    const conflict = await donate(page, name);
    expect(conflict.status()).toBe(409);
    const confirmation = page.getByRole("dialog", {
      name: "是否为再次赞赏？",
      exact: true,
    });
    await expect(confirmation).toContainText(name);
    await confirmation
      .getByRole("button", { name: "更换昵称", exact: true })
      .click();
    await expect(page.locator("#donate-nickname")).toBeEditable();
    await expect(page.locator("#donate-nickname")).toHaveValue(name);
    const before = (await (
      await page.request.get(`${base()}/api/admin/donations?group_by=nickname`)
    ).json()) as DonationList;
    expect(before.items.filter((item) => item.nickname === name)).toHaveLength(
      1,
    );

    await page
      .getByRole("button", { name: "确认金额并赞赏", exact: true })
      .click();
    await expect(confirmation).toBeVisible();
    await page.setViewportSize({ width: 320, height: 800 });
    await expect(confirmation).toHaveCSS("opacity", "1");
    const dialogBox = await confirmation.boundingBox();
    expect(dialogBox!.x).toBeGreaterThanOrEqual(0);
    expect(dialogBox!.x + dialogBox!.width).toBeLessThanOrEqual(321);
    await page.screenshot({
      path: "../.local/donation-repeat-confirm-320.png",
      fullPage: true,
    });
    const repeated = page.waitForResponse(
      (response) =>
        response.url() === `${base()}/api/donations` &&
        response.status() === 201,
    );
    await confirmation
      .getByRole("button", { name: "确认再次赞赏", exact: true })
      .click();
    const second = ((await (await repeated).json()) as { donation: Donation })
      .donation;
    expect(second.id).not.toBe(first.id);
    await expect(page).toHaveURL(`${base()}/donate/orders/${second.id}`);
    await expect(
      page.getByRole("button", { name: "前往 HashPay 付款", exact: true }),
    ).toBeEnabled({ timeout: 30_000 });

    // 增加真实昵称数据，覆盖卡片数量、长昵称、emoji 和待付款展示。
    for (const label of [
      "小林",
      "热心网友🌱",
      "一杯咖啡",
      "支持开源项目的朋友".repeat(5),
    ]) {
      const response = await page.request.post(`${base()}/api/donations`, {
        headers: { Origin: base() },
        data: {
          request_id: crypto.randomUUID().replaceAll("-", ""),
          nickname: `${label}-${Date.now()}`,
          show_public: false,
          amount: 500,
          currency: "CNY",
        },
      });
      expect(response.ok(), await response.text()).toBeTruthy();
    }
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(`${base()}/center/overview`);
    const wall = page.getByRole("region", { name: "赞赏记录", exact: true });
    const card = wall.locator(".supporter-card").filter({ hasText: name });
    await expect(card).toHaveCount(1);
    await expect(card.locator(".supporter-amounts")).toContainText("5.00 CNY");
    await expect(card.locator("summary")).toContainText("2 笔赞赏");
    const metrics = await page.locator(".metric-row").boundingBox();
    const wallBox = await wall.boundingBox();
    expect(wallBox!.y).toBeGreaterThan(metrics!.y + metrics!.height);
    await page.mouse.move(0, 0);
    const positions = await card.evaluate(async (element) => {
      const first = getComputedStyle(element).transform;
      await new Promise((resolve) => setTimeout(resolve, 250));
      return [first, getComputedStyle(element).transform];
    });
    expect(positions[0]).not.toBe(positions[1]);
    await wall.locator(".supporter-field").hover({ position: { x: 8, y: 8 } });
    await card.locator("summary").click();
    await expect(card.locator(".donation-record")).toHaveCount(2);
    expect(
      await card.evaluate(
        (element) => getComputedStyle(element).animationPlayState,
      ),
    ).toBe("paused");
    const pending = card
      .locator(".donation-record")
      .filter({ hasText: second.id });
    await expect(pending).toContainText("等待付款");
    expect(
      (await request.post(`${base()}/__test/pay-donation/${second.id}`)).ok(),
    ).toBeTruthy();
    await pending
      .getByRole("button", { name: "刷新状态", exact: true })
      .click();
    await expect(card.locator(".supporter-amounts")).toContainText("10.00 CNY");
    await expect(card.locator("details")).toHaveAttribute("open", "");
    await expect(pending).toContainText("已付款");
    await card.locator("summary").click();
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      expect(
        await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth + 1,
        ),
      ).toBeFalsy();
      const bounds = await wall.evaluate((element) => {
        const parent = element.getBoundingClientRect();
        return [...element.querySelectorAll(".supporter-card")].every(
          (card) => {
            const rect = card.getBoundingClientRect();
            return (
              rect.left >= parent.left &&
              rect.right <= parent.right &&
              rect.top >= parent.top &&
              rect.bottom <= parent.bottom
            );
          },
        );
      });
      expect(bounds).toBeTruthy();
      await page.screenshot({
        path: `../.local/donation-wall-${width}.png`,
        fullPage: true,
      });
    }
    await page.emulateMedia({ reducedMotion: "reduce" });
    expect(
      await card.evaluate((element) => getComputedStyle(element).animationName),
    ).toBe("none");
    await page.goto(`${base()}/center/donation`);
    await expect(page.locator(".donation-wall")).toHaveCount(0);

    const remote = await browser.newContext({ locale: "zh-CN" });
    try {
      const querier = await remote.newPage();
      await querier.goto(`${base()}/center/donations`);
      await querier.getByRole("button", { name: "查询", exact: true }).click();
      await querier.locator("#donation-query-nickname").fill(name);
      await querier
        .getByRole("dialog")
        .getByRole("button", { name: "查询", exact: true })
        .click();
      await expect(querier.getByRole("dialog")).toHaveCount(0);
      await expect(querier).toHaveURL(`${base()}/center/donations`);
      await expect(querier.locator(".donation-history-row")).toHaveCount(2);
      await querier.reload();
      await expect(querier.locator(".donation-history-row")).toHaveCount(2);
    } finally {
      await remote.close();
    }
  } finally {
    await restore();
  }
});

test("重复赞赏的网络失败保留请求，查询同昵称旧记录不会误清除，响应丢失后仍恢复同一笔", async ({
  page,
}) => {
  const restore = await configure(page);
  const name = uniqueName();
  try {
    const first = await donate(page, name);
    expect(first.ok()).toBeTruthy();
    await expect(page).toHaveURL(/\/donate\/orders\//);
    let requestID = "",
      createdID = "",
      confirmedCalls = 0;
    await page.route(`${base()}/api/donations`, async (route) => {
      const input = route.request().postDataJSON();
      if (!input.confirm_repeat) return route.continue();
      if (!requestID) requestID = input.request_id;
      expect(input.request_id).toBe(requestID);
      confirmedCalls++;
      if (confirmedCalls === 1) return route.abort("failed");
      const response = await route.fetch();
      if (confirmedCalls === 2) {
        createdID = ((await response.json()) as { donation: Donation }).donation
          .id;
        return route.abort("failed");
      }
      await route.fulfill({ response });
    });
    expect((await donate(page, name)).status()).toBe(409);
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "确认再次赞赏", exact: true })
      .click();
    await expect(
      page.getByRole("alert").filter({ hasText: "网络请求失败" }),
    ).toBeVisible();
    await page.goto(`${base()}/center/donations`);
    await page.getByRole("button", { name: "查询", exact: true }).click();
    await page.locator("#donation-query-nickname").fill(name);
    await page
      .getByRole("dialog")
      .getByRole("button", { name: "查询", exact: true })
      .click();
    await expect(page).toHaveURL(/\/center\/donations\//);
    await page.goto(`${base()}/donate`);
    await expect(page.locator("#donate-nickname")).toBeDisabled();
    await page
      .getByRole("button", { name: "确认金额并赞赏", exact: true })
      .click();
    await expect(
      page.getByRole("alert").filter({ hasText: "网络请求失败" }),
    ).toBeVisible();
    await page.reload();
    await expect(page.locator("#donate-nickname")).toBeDisabled();
    await page
      .getByRole("button", { name: "确认金额并赞赏", exact: true })
      .click();
    await expect(page).toHaveURL(`${base()}/donate/orders/${createdID}`);
    expect(confirmedCalls).toBe(3);
    const records = (await (
      await page.request.get(`${base()}/api/admin/donations?group_by=nickname`)
    ).json()) as DonationList;
    expect(records.items.filter((item) => item.nickname === name)).toHaveLength(
      2,
    );
  } finally {
    await restore();
  }
});

test("昵称查询跨页导入全部历史，重新进入页面后仍可分页查看", async ({
  page,
}) => {
  const name = uniqueName();
  const records: Donation[] = Array.from({ length: 23 }, (_, index) => ({
    id: `don_${String(index).padStart(22, "a")}`,
    title: "长期支持",
    nickname: name,
    show_public: false,
    amount: 500,
    currency: "CNY",
    gateway_id: "",
    checkout_url: "",
    payment_status: "paid",
    expires_at: 0,
    paid_at: 1000 - index,
    created_at: 1000 - index,
    last_error: "",
  }));
  const pages: number[] = [];
  await page.route(`${base()}/api/donations/lookup`, (route) => {
    const { page: current } = route.request().postDataJSON();
    pages.push(current);
    return route.fulfill({
      json: {
        items: records.slice((current - 1) * 20, current * 20),
        page: current,
        has_more: current === 1,
        matched_request_id: "",
      },
    });
  });
  await page.goto(`${base()}/center/donations`);
  await page.getByRole("button", { name: "查询", exact: true }).click();
  await page.locator("#donation-query-nickname").fill(name);
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "查询", exact: true })
    .click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  expect(pages).toEqual([1, 2]);
  await expect(page.locator(".donation-history-row")).toHaveCount(20);
  await page.locator(".n-pagination-item").filter({ hasText: /^2$/ }).click();
  await expect(page.locator(".donation-history-row")).toHaveCount(3);
  await expect(page.locator(".personal-donations")).toContainText(
    records[22]!.id,
  );
  await page.reload();
  await expect(page.locator(".donation-history-row")).toHaveCount(20);
  await expect(
    page.getByRole("button", { name: "清除本地捐赠记录", exact: true }),
  ).toBeEnabled();
  await page.locator(".n-pagination-item").filter({ hasText: /^2$/ }).click();
  await expect(page.locator(".donation-history-row")).toHaveCount(3);
});
