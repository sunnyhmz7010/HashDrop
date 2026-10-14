import { test as base } from "@playwright/test";

export { expect, type Locator, type Page } from "@playwright/test";

export const test = base.extend<{ isolateLimits: void }>({
  isolateLimits: [
    async ({ request }, use) => {
      const response = await request.post(
        `${process.env.TINYFAKA_TEST_URL!}/__test/reset-limits`,
      );
      if (!response.ok()) throw new Error("测试请求计数重置失败");
      await use();
    },
    { auto: true },
  ],
});
