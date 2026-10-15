import { test as base, expect, type Page } from "@playwright/test";
import type { Inventory } from "../src/api";

export { expect, type Locator, type Page } from "@playwright/test";

// 为不涉及库存创建的用例准备独立库存，保留实际 API、鉴权和数据库路径。
export async function createInventory(
  page: Page,
  name: string,
  kind = "card",
  content = "",
): Promise<Inventory> {
  await expect(page.locator(".workspace-frame")).toBeVisible();
  const baseURL = process.env.TINYFAKA_TEST_URL!;
  const session = await (
    await page.request.get(`${baseURL}/api/admin/session`)
  ).json();
  const response = await page.request.post(`${baseURL}/api/admin/inventories`, {
    headers: { Origin: baseURL, "X-CSRF-Token": session.csrf },
    data: { name, kind, static_content: content },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  return response.json();
}

export async function selectProductInventory(page: Page, pool: Inventory) {
  await page.locator("#product-inventory").click();
  await page
    .locator(".n-base-select-option")
    .filter({ hasText: pool.name })
    .click();
}

export async function importInventory(
  page: Page,
  pool: Inventory,
  content: string,
) {
  await page.goto(`${process.env.TINYFAKA_TEST_URL!}/admin/inventories`);
  const row = page.getByRole("row").filter({ hasText: pool.id });
  await row.getByRole("button", { name: "导入", exact: true }).click();
  await page.getByPlaceholder("每行一份库存").fill(content);
  await page.getByRole("button", { name: "导入库存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
}

export async function loginAdmin(page: Page) {
  await page.goto(`${process.env.TINYFAKA_TEST_URL!}/admin/login`);
  await page.locator("#password input").fill("test-password-only");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.locator(".workspace-frame")).toBeVisible();
}

export const test = base.extend<{ browserState: void }>({
  browserState: [
    async ({ request, context, page }, use) => {
      const baseURL = process.env.TINYFAKA_TEST_URL!;
      const response = await request.post(`${baseURL}/__test/reset-limits`);
      if (!response.ok()) throw new Error("测试请求计数重置失败");
      const errors: string[] = [];
      const watch = (target: Page) =>
        target.on("pageerror", (error) => errors.push(error.message));
      watch(page);
      context.on("page", watch);
      await use();
      expect(errors).toEqual([]);
    },
    { auto: true },
  ],
});
