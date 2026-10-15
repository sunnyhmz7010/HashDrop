import { test, expect, type Locator } from "./fixture";
import { placeTestOrder } from "./checkout";

async function verifyRichContent(content: Locator) {
  await expect(
    content.getByRole("heading", { name: "容器语法示例" }),
  ).toBeVisible();
  await expect(
    content.locator("strong").filter({ hasText: "详细说明" }),
  ).toContainText("详细说明");
  await expect(content.locator("table")).toHaveCount(1);
  await expect(content.locator("s, del")).toHaveText([
    "旧版本说明",
    "单波浪号说明",
  ]);
  await expect(
    content.locator("script, iframe, object, svg, form, style"),
  ).toHaveCount(0);
  await expect(
    content.locator("[onerror], [onclick], [onload], [style], [name]"),
  ).toHaveCount(0);
  // 标签页的 ID 由渲染器生成，用户内容中的 id 必须继续被清理。
  await expect(
    content.locator('[id]:not([role="tab"]):not([role="tabpanel"])'),
  ).toHaveCount(0);
  await expect(
    content.locator(
      'a[href^="javascript:"], img[src^="data:"], video[autoplay]',
    ),
  ).toHaveCount(0);
  await expect(
    content.getByText("危险链接", { exact: true }),
  ).not.toHaveAttribute("href");
  const tasks = content.locator('input[type="checkbox"]');
  await expect(tasks).toHaveCount(2);
  for (const task of await tasks.all()) {
    await expect(task).toBeDisabled();
    await expect(task).toBeChecked();
  }
  await expect(
    content.locator("li").filter({ hasText: "加粗内容" }).locator("input"),
  ).toHaveCount(0);
  await expect(content.locator(".markdown-container-warning")).toContainText(
    "购买须知",
  );
  await expect(
    content.locator(".markdown-container-warning .markdown-container-tip"),
  ).toContainText("嵌套提示");
  await expect(content.locator(".markdown-container-custom")).toContainText(
    "补充信息",
  );
  await expect(content.locator(".topbar, .form-modal")).toHaveCount(0);
  await expect(
    content.locator("pre").filter({ hasText: "这是代码" }),
  ).toContainText("::: warning");
  const fold = content.locator("details");
  await expect(fold.getByText("折叠正文", { exact: true })).toBeHidden();
  await fold.locator("summary").click();
  await expect(fold.getByText("折叠正文", { exact: true })).toBeVisible();
  await fold.locator("summary").click();
  const firstTab = content.getByRole("tab", {
    name: "💻基本信息",
    exact: true,
  });
  const secondTab = content.getByRole("tab", { name: "🎬IP质量", exact: true });
  await expect(firstTab).toHaveAttribute("aria-selected", "true");
  await expect(
    content.getByText("硬件报告正文", { exact: true }),
  ).toBeVisible();
  await expect(content.getByText("IP 报告正文", { exact: true })).toBeHidden();
  await secondTab.click();
  await expect(secondTab).toHaveAttribute("aria-selected", "true");
  await expect(content.getByText("IP 报告正文", { exact: true })).toBeVisible();
  await expect(content.getByText("硬件报告正文", { exact: true })).toBeHidden();
  await secondTab.press("ArrowLeft");
  await expect(firstTab).toBeFocused();
  await expect(firstTab).toHaveAttribute("aria-selected", "true");
}

test("Markdown 详情保存、图片视频展示与首页直接下单", async ({ page }) => {
  test.setTimeout(60_000);
  const base = process.env.TINYFAKA_TEST_URL!;
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(base);
  const name = "Markdown 详情示例";
  const image = `${base}/__test/media/image.svg`;
  const video = `${base}/__test/media/clip.webm?demo=1`;
  const details = [
    "## 容器语法示例",
    "这是一份支持 **详细说明** 的商品。",
    "~~旧版本说明~~",
    "~单波浪号说明~",
    "| 项目 | 内容 |\n| --- | --- |\n| 格式 | Markdown |",
    ":::: warning 购买须知\n请先确认商品适合你的用途。\n\n::: tip 温馨提示\n**嵌套提示**：请保存订单领取链接。\n:::\n::::",
    "::: custom 补充信息\n自定义容器也支持普通 Markdown。\n:::",
    `![商品展示](${image})`,
    "### 演示视频",
    `::: info 视频预览\n![商品视频](${video})\n:::`,
    "::: details 更多说明\n**折叠正文**\n:::",
    ":::: tabs\n::: tab-item 💻基本信息\n```ansi\n硬件报告正文\n```\n:::\n::: tab-item 🎬IP质量\n**IP 报告正文**\n:::\n::::",
    "> 购买前请查看商品说明。",
    "- [x] 支持图片与视频",
    "- [x] 已完成清单\n\n[x]: https://example.com",
    "- [x]**加粗内容**",
    "```text\n" + "long-code-".repeat(40) + "\n```",
    "```markdown\n::: warning\n这是代码，不能变成提示块。\n:::\n```",
    `<script>throw new Error('unsafe-script')</script>`,
    `<iframe src="${base}"></iframe>`,
    `<img src="${image}" onerror="alert(1)" style="position:fixed" id="evil" name="evil">`,
    '<a href="javascript:alert(1)" onclick="alert(1)">危险链接</a>',
    '<video src="javascript:alert(1)" autoplay></video>',
    '<img src="data:image/svg+xml,<svg onload=alert(1)></svg>">',
    '<div class="topbar form-modal" id="evil-container" style="position:fixed">不允许借用站点样式</div>',
  ].join("\n\n");
  await page.goto(`${base}/admin/login`);
  await page.locator("#password input").fill("test-password-only");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.getByRole("menuitem", { name: "商品与库存", exact: true }).click();
  await page.getByRole("button", { name: "新增商品" }).click();
  const editor = page.getByRole("dialog", { name: "新增商品", exact: true });
  await editor.locator("#product-name input").fill(name);
  await editor.locator("#product-kind").click();
  await page
    .locator(".n-base-select-option")
    .getByText("固定文本", { exact: true })
    .click();
  await editor.locator("#price input").fill("12.34");
  await editor.locator("#active").click();
  await editor
    .locator("#description textarea")
    .fill("商品介绍在首页和详情顶部显示");
  await editor.locator("#product-details-input").fill(details);
  await editor.locator("#static-content textarea").fill("测试交付内容");
  await editor
    .locator(".n-tabs-tab")
    .getByText("预览", { exact: true })
    .click();
  const preview = editor.locator(".markdown-content");
  await preview.scrollIntoViewIfNeeded();
  await verifyRichContent(preview);
  await page.screenshot({
    path: "../.local/product-editor-preview.png",
    animations: "disabled",
  });
  const saved = page.waitForResponse(
    (response) =>
      response.url() === `${base}/api/admin/products` &&
      response.request().method() === "POST",
  );
  await editor.getByRole("button", { name: "保存商品", exact: true }).click();
  const product = await (await saved).json();
  expect(product.details).toBe(details);
  const row = page.getByRole("row").filter({ hasText: name });
  await row.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(page.locator("#product-details-input")).toHaveValue(details);
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await page.goto(base);
  const card = page.locator(".product-card").filter({ hasText: name });
  await expect(card).toContainText("商品介绍在首页和详情顶部显示");
  const view = card.getByRole("link", { name: "详情", exact: true });
  const buy = card.getByRole("button", { name: "下单", exact: true });
  for (const control of [view, buy]) {
    expect((await control.innerText()).trim()).toBe("");
    await expect(control.locator("svg")).toHaveCount(1);
  }
  await view.click();
  await expect(page).toHaveURL(`${base}/products/${product.id}`);
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByLabel("购买数量")).toHaveCount(0);
  await expect(page.locator(".product-summary")).toContainText(
    "商品介绍在首页和详情顶部显示",
  );
  const content = page.locator(".product-details .markdown-content");
  await expect(content).not.toContainText("商品介绍在首页和详情顶部显示");
  await verifyRichContent(content);
  await content.getByRole("img", { name: "商品展示" }).scrollIntoViewIfNeeded();
  await expect
    .poll(() =>
      content
        .getByRole("img", { name: "商品展示" })
        .evaluate((el: HTMLImageElement) => el.naturalWidth),
    )
    .toBe(640);
  const player = content.locator("video");
  await expect(player).toHaveCount(1);
  await expect(player).toHaveAttribute("controls", "");
  await expect
    .poll(() =>
      player.evaluate((el: HTMLVideoElement) => {
        if (el.error)
          throw new Error(
            `视频加载失败 ${el.error.code}: ${el.error.message}; src=${el.currentSrc}`,
          );
        return el.readyState;
      }),
    )
    .toBeGreaterThanOrEqual(2);
  await player.evaluate(async (el: HTMLVideoElement) => {
    await el.play();
  });
  await expect
    .poll(() => player.evaluate((el: HTMLVideoElement) => el.currentTime))
    .toBeGreaterThan(0);
  await player.evaluate((el: HTMLVideoElement) => el.pause());
  for (const width of [1440, 768, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
    ).toBeFalsy();
    const top = (await page.locator(".product-summary").boundingBox())!;
    const bottom = (await page.locator(".product-details").boundingBox())!;
    expect(bottom.y).toBeGreaterThanOrEqual(top.y + top.height);
    expect(bottom.x).toBe(top.x);
    if (width === 1440 || width === 390) {
      await page.locator(".product-summary").scrollIntoViewIfNeeded();
      if (width === 1440) {
        await page.screenshot({
          path: "../.local/markdown-containers-desktop.png",
          animations: "disabled",
        });
      }
      await page.screenshot({
        path: `../.local/product-details-${width === 1440 ? "desktop" : "mobile"}.png`,
        fullPage: true,
        animations: "disabled",
      });
    }
  }
  await page.getByRole("button", { name: "下单", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "确认下单" })).toBeInViewport();
  await page.getByRole("button", { name: "取消", exact: true }).click();
  await page.goto(base);
  await page.setViewportSize({ width: 1440, height: 1000 });
  let posts = 0;
  await page.route("**/api/orders", async (route) => {
    if (route.request().method() === "POST") {
      posts++;
      await new Promise((resolve) => setTimeout(resolve, 600));
    }
    await route.continue();
  });
  await buy.click();
  await expect(page).toHaveURL(`${base}/`);
  const purchase = page.getByRole("dialog", { name: "确认下单", exact: true });
  await expect(
    purchase.getByRole("button", { name: "前往支付" }),
  ).toBeDisabled();
  await purchase.getByRole("button", { name: "取消" }).click();
  expect(posts).toBe(0);
  // 初次读取失败允许重新查询；提交失败保留数量且不跳转。
  await page.route(`**/api/products/${product.id}`, (route) =>
    route.fulfill({ status: 503, json: { error: "详情加载暂时失败" } }),
  );
  await buy.click();
  await expect(purchase.getByText("详情加载暂时失败")).toBeVisible();
  await expect(
    purchase.getByRole("button", { name: "前往支付" }),
  ).toBeDisabled();
  await page.unroute(`**/api/products/${product.id}`);
  await purchase.getByRole("button", { name: "重新加载" }).click();
  await purchase.getByRole("button", { name: "生成", exact: true }).click();
  await expect(
    purchase.getByRole("button", { name: "前往支付" }),
  ).toBeEnabled();
  await purchase.getByLabel("购买数量").fill("2");
  await purchase.getByLabel("购买数量").blur();
  await expect(purchase.locator(".purchase-total")).toContainText("24.68 CNY");
  await page.route("**/api/orders", (route) =>
    route.fulfill({ status: 503, json: { error: "订单创建暂时失败" } }),
  );
  await purchase.getByRole("button", { name: "前往支付" }).click();
  await expect(purchase.getByText("订单创建暂时失败")).toBeVisible();
  await expect(purchase.getByLabel("购买数量")).toHaveValue("2");
  await page.unroute("**/api/orders");
  await page.route("**/api/orders", async (route) => {
    posts++;
    await new Promise((resolve) => setTimeout(resolve, 600));
    await route.continue();
  });
  await page.screenshot({
    path: "../.local/purchase-dialog-desktop.png",
    animations: "disabled",
  });
  const placing = placeTestOrder(page);
  await expect(
    purchase.getByRole("button", { name: "继续获取支付链接" }),
  ).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(purchase).toBeVisible();
  const { orderID, key: token } = await placing;
  expect(posts).toBe(1);
  expect(token).toHaveLength(24);
  const order = await (
    await page.request.post(`${base}/api/orders/lookup`, {
      data: { key: token },
    })
  ).json();
  expect(order.order.quantity).toBe(2);
  expect(order.order.total).toBe(2468);
  // 新商品只服务本用例，不影响其他规格对固定文本商品的选择。
  const session = await (
    await page.request.get(`${base}/api/admin/session`)
  ).json();
  const deactivated = await page.request.put(
    `${base}/api/admin/products/${product.id}`,
    {
      headers: { Origin: base, "X-CSRF-Token": session.csrf },
      data: {
        name: product.name,
        description: product.description,
        details: product.details,
        price: product.price,
        currency: product.currency,
        kind: product.kind,
        active: false,
      },
    },
  );
  expect(deactivated.ok()).toBeTruthy();
  expect(errors).toEqual([]);
});
