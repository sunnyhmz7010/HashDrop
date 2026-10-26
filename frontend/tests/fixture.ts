import { test as base, expect, type Page } from "@playwright/test";
import type { ContentDocument, Inventory, Product } from "../src/api";

export { expect, type Locator, type Page } from "@playwright/test";

// 为不涉及库存创建的用例准备独立库存，保留实际 API、鉴权和数据库路径。
export async function createInventory(
  page: Page,
  name: string,
  reusable = false,
  content = "",
): Promise<Inventory> {
  await expect(page.locator(".workspace-frame")).toBeVisible();
  const baseURL = process.env.HASHDROP_TEST_URL!;
  const session = await (
    await page.request.get(`${baseURL}/api/admin/session`)
  ).json();
  const response = await page.request.post(`${baseURL}/api/admin/inventories`, {
    headers: { Origin: baseURL, "X-CSRF-Token": session.csrf },
    data: { name, reusable, bindings: [] },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  const pool: Inventory = await response.json();
  if (content) {
    const imported = await page.request.post(
      `${baseURL}/api/admin/inventories/${pool.id}/stock`,
      {
        headers: { Origin: baseURL, "X-CSRF-Token": session.csrf },
        data: { items: [testContent(content)] },
      },
    );
    expect(imported.ok(), await imported.text()).toBeTruthy();
  }
  return pool;
}

export async function bindInventoryProducts(
  page: Page,
  pool: Inventory,
  products: string[],
) {
  const baseURL = process.env.HASHDROP_TEST_URL!;
  const session = await (
    await page.request.get(`${baseURL}/api/admin/session`)
  ).json();
  const catalog: Product[] = await (
    await page.request.get(`${baseURL}/api/admin/products`)
  ).json();
  const bindings = products.flatMap((product_id) => {
    const product = catalog.find((item) => item.id === product_id)!;
    return (product.delivery_plan?.blocks ?? [])
      .filter((block) => block.type === "stock")
      .map((block) => ({ product_id, slot_id: block.id }));
  });
  const response = await page.request.put(
    `${baseURL}/api/admin/inventories/${pool.id}`,
    {
      headers: { Origin: baseURL, "X-CSRF-Token": session.csrf },
      data: { name: pool.name, reusable: pool.reusable, bindings },
    },
  );
  expect(response.ok(), await response.text()).toBeTruthy();
}

export function testContent(text: string): ContentDocument {
  return { blocks: [{ id: "text", type: "text", text }] };
}
export function testPlan(): ContentDocument {
  return {
    blocks: [{ id: "stock", type: "stock", label: "交付内容", quantity: 1 }],
  };
}
export async function claimContents(page: Page) {
  await expect(page.locator(".delivery-item").first()).toBeVisible();
  const claim = page.getByRole("button", {
    name: /^(领取内容|Claim content)$/,
  });
  while (await claim.count()) {
    const remaining = await claim.count();
    await claim.first().click();
    await expect(claim).toHaveCount(remaining - 1);
  }
}

export async function importInventory(
  page: Page,
  pool: Inventory,
  content: string,
) {
  await page.goto(`${process.env.HASHDROP_TEST_URL!}/center/inventories`);
  const row = page.getByRole("row").filter({ hasText: pool.id });
  await row.getByRole("button", { name: "导入", exact: true }).click();
  await page.getByPlaceholder("每行一份库存").fill(content);
  await page.getByRole("button", { name: "导入库存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
}

export async function loginAdmin(page: Page) {
  await page.goto(`${process.env.HASHDROP_TEST_URL!}/center?login=1`);
  await page.locator("#password input").fill("test-password-only");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(
    page.getByRole("button", { name: /^(退出登录|Sign out)$/ }),
  ).toBeVisible();
  await expect(page.getByRole("dialog")).toHaveCount(0);
}

export const test = base.extend<{ browserState: void }>({
  browserState: [
    async ({ request, context, page }, use) => {
      const baseURL = process.env.HASHDROP_TEST_URL!;
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
