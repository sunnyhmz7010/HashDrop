<script setup lang="ts">
import { computed, ref, useId } from "vue";
import createDOMPurify from "dompurify";
import MarkdownIt, { type Delimiter, type RendererRule } from "markdown-it";
import container from "markdown-it-container";
import { t } from "../i18n";

const props = defineProps<{ content: string }>();
const contentRoot = ref<HTMLElement>();
const instanceID = useId();
let nextTabsID = 0;
const parser = new MarkdownIt({
  html: true,
  linkify: true,
  breaks: true,
});
parser.linkify.set({ fuzzyLink: true });
const containerHeader = /^([a-z][a-z0-9-]*)(?:\s+(.+))?$/;
const renderContainer: RendererRule = (tokens, index) => {
  if (tokens[index].nesting === -1) return "</div>\n";
  const [, name, title] = tokens[index].info.trim().match(containerHeader)!;
  const label = title
    ? `<p class="markdown-container-title">${parser.utils.escapeHtml(title)}</p>\n`
    : "";
  return `<div class="markdown-container markdown-container-${name}">\n${label}`;
};
parser.use(container, "container", {
  validate: (params: string) => {
    const header = params.trim().match(containerHeader);
    return !!header && !["details", "spoiler"].includes(header[1]);
  },
  render: renderContainer,
});
for (const name of ["details", "spoiler"]) {
  const render: RendererRule = (tokens, index) => {
    if (tokens[index].nesting === -1) return "</details>\n";
    const title =
      tokens[index].info.trim().slice(name.length).trim() || t("shop.view");
    return `<details class="markdown-container markdown-container-details"><summary>${parser.utils.escapeHtml(title)}</summary>\n`;
  };
  parser.use(container, name, { render });
}
// 先按原始段落识别任务标记，避免 [x] 被引用链接解析，或误判紧跟加粗的 [x]。
parser.core.ruler.before("inline", "task_list_marker", (state) => {
  for (let index = 2; index < state.tokens.length; index++) {
    const token = state.tokens[index];
    if (
      token.type !== "inline" ||
      state.tokens[index - 1].type !== "paragraph_open" ||
      state.tokens[index - 2].type !== "list_item_open"
    )
      continue;
    const marker = token.content.match(/^\[([ xX])\](?:\s+|$)/);
    if (!marker) continue;
    token.meta = {
      ...token.meta,
      taskChecked: marker[1].toLowerCase() === "x",
    };
    token.content = token.content.slice(marker[0].length);
  }
});
parser.core.ruler.after("inline", "task_list_checkbox", (state) => {
  for (const token of state.tokens) {
    if (typeof token.meta?.taskChecked !== "boolean" || !token.children)
      continue;
    const checkbox = new state.Token("html_inline", "", 0);
    checkbox.content = `<input type="checkbox" disabled${token.meta.taskChecked ? " checked" : ""}> `;
    token.children.unshift(checkbox);
  }
});
// 兼容旧内容的单波浪号删除线。独立分隔符组避免与原生 ~~ 删除线混配。
const singleStrikeMarker = 0x7f;
parser.inline.ruler.before(
  "strikethrough",
  "single_strike",
  (state, silent) => {
    if (silent || state.src[state.pos] !== "~") return false;
    const scanned = state.scanDelims(state.pos, true);
    if (scanned.length !== 1) return false;
    state.push("text", "", 0).content = "~";
    state.delimiters.push({
      marker: singleStrikeMarker,
      length: 0,
      token: state.tokens.length - 1,
      end: -1,
      open: scanned.can_open,
      close: scanned.can_close,
    });
    state.pos++;
    return true;
  },
);
parser.inline.ruler2.after("strikethrough", "single_strike", (state) => {
  const apply = (delimiters: Delimiter[]) => {
    for (const delimiter of delimiters) {
      if (delimiter.marker !== singleStrikeMarker || delimiter.end === -1)
        continue;
      const open = state.tokens[delimiter.token];
      const close = state.tokens[delimiters[delimiter.end].token];
      Object.assign(open, {
        type: "s_open",
        tag: "s",
        nesting: 1,
        content: "",
      });
      Object.assign(close, {
        type: "s_close",
        tag: "s",
        nesting: -1,
        content: "",
      });
    }
  };
  apply(state.delimiters);
  for (const meta of state.tokens_meta)
    if (meta?.delimiters) apply(meta.delimiters);
});
const renderImage = parser.renderer.rules.image!;
parser.renderer.rules.image = (tokens, index, options, env, renderer) => {
  // 视频直链沿用图片语法，查询参数不影响文件类型识别。
  const href = String(tokens[index].attrGet("src") ?? "");
  let url: URL;
  try {
    url = new URL(href, location.origin);
  } catch {
    return renderImage(tokens, index, options, env, renderer);
  }
  if (!/\.(mp4|webm|ogv)$/i.test(url.pathname))
    return renderImage(tokens, index, options, env, renderer);
  const label = renderer.renderInlineAsText(
    tokens[index].children ?? [],
    options,
    env,
  );
  return `<video src="${parser.utils.escapeHtml(href)}" aria-label="${parser.utils.escapeHtml(label)}" controls preload="metadata"></video>`;
};
// 每个渲染器使用独立清理实例，编辑预览和公开详情遵循相同的规则。
const purifier = createDOMPurify(window);
function safeURL(value: string, media: boolean): boolean {
  if (!value.trim()) return false;
  try {
    const url = new URL(value, location.origin);
    if (url.username || url.password) return false;
    return (
      url.protocol === "https:" ||
      (url.protocol === "http:" && (!media || url.origin === location.origin))
    );
  } catch {
    return false;
  }
}
purifier.addHook("afterSanitizeAttributes", (node) => {
  if (!(node instanceof Element)) return;
  if (node.hasAttribute("class")) {
    // 只保留本文渲染器的容器类，避免富文本借用站点顶栏、弹窗等样式。
    const classes = node
      .getAttribute("class")!
      .split(/\s+/)
      .filter(
        (name) =>
          (["DIV", "DETAILS"].includes(node.tagName) &&
            /^markdown-container(?:-[a-z][a-z0-9-]*)?$/.test(name)) ||
          (node.tagName === "P" && name === "markdown-container-title"),
      );
    if (classes.length) node.setAttribute("class", classes.join(" "));
    else node.removeAttribute("class");
  }
  if (node.tagName === "A" && node.hasAttribute("href")) {
    if (!safeURL(node.getAttribute("href")!, false))
      node.removeAttribute("href");
    else {
      node.setAttribute("target", "_blank");
      node.setAttribute("rel", "noopener noreferrer nofollow");
    }
  }
  if (["IMG", "VIDEO", "SOURCE"].includes(node.tagName)) {
    if (node.hasAttribute("src") && !safeURL(node.getAttribute("src")!, true)) {
      node.remove();
      return;
    }
    if (
      node.hasAttribute("poster") &&
      !safeURL(node.getAttribute("poster")!, true)
    )
      node.removeAttribute("poster");
    if (node.tagName === "IMG") {
      if (!node.hasAttribute("src")) {
        node.remove();
        return;
      }
      node.setAttribute("loading", "lazy");
      node.setAttribute("decoding", "async");
      node.setAttribute("referrerpolicy", "no-referrer");
    }
    if (node.tagName === "VIDEO") {
      if (
        !node.hasAttribute("src") &&
        !Array.from(node.querySelectorAll("source[src]")).some((source) =>
          safeURL(source.getAttribute("src")!, true),
        )
      ) {
        node.remove();
        return;
      }
      node.setAttribute("controls", "");
      node.setAttribute("preload", "metadata");
    }
  }
  if (node.tagName === "INPUT") {
    if (node.getAttribute("type") !== "checkbox") node.remove();
    else node.setAttribute("disabled", "");
  }
});
const html = computed(() =>
  renderTabs(
    String(
      purifier.sanitize(parser.render(props.content), {
        ALLOWED_TAGS: [
          "p",
          "br",
          "h1",
          "h2",
          "h3",
          "h4",
          "h5",
          "h6",
          "strong",
          "em",
          "del",
          "s",
          "blockquote",
          "ul",
          "ol",
          "li",
          "hr",
          "pre",
          "code",
          "a",
          "img",
          "video",
          "source",
          "table",
          "thead",
          "tbody",
          "tr",
          "th",
          "td",
          "input",
          "div",
          "details",
          "summary",
        ],
        ALLOWED_ATTR: [
          "href",
          "src",
          "alt",
          "title",
          "poster",
          "controls",
          "preload",
          "type",
          "checked",
          "disabled",
          "start",
          "colspan",
          "rowspan",
          "aria-label",
          "loading",
          "decoding",
          "referrerpolicy",
          "target",
          "rel",
          "class",
          "open",
        ],
        ALLOW_DATA_ATTR: false,
        ALLOW_ARIA_ATTR: false,
      }),
    ),
  ),
);

function renderTabs(safeHTML: string): string {
  // 先清理用户内容，再用 DOM API 生成交互元素，不放行用户自带的事件和 ID。
  const document = new DOMParser().parseFromString(safeHTML, "text/html");
  const groups = Array.from(
    document.body.querySelectorAll<HTMLElement>(".markdown-container-tabs"),
  );
  for (const group of groups) {
    const items = Array.from(group.children).filter(
      (child): child is HTMLElement =>
        child instanceof HTMLElement &&
        child.classList.contains("markdown-container-tab-item"),
    );
    if (!items.length) continue;

    const groupID = `markdown-tabs-${instanceID}-${nextTabsID++}`;
    const list = document.createElement("div");
    list.className = "markdown-tabs-list";
    list.setAttribute("role", "tablist");
    const panels = document.createElement("div");
    panels.className = "markdown-tabs-panels";

    group.classList.remove("markdown-container", "markdown-container-tabs");
    group.classList.add("markdown-tabs");

    items.forEach((item, index) => {
      const itemTitle = Array.from(item.children).find((child) =>
        child.classList.contains("markdown-container-title"),
      );
      const label = itemTitle?.textContent?.trim() || String(index + 1);
      itemTitle?.remove();

      const tabID = `${groupID}-tab-${index}`;
      const panelID = `${groupID}-panel-${index}`;
      const button = document.createElement("button");
      button.className = "markdown-tab";
      button.type = "button";
      button.id = tabID;
      button.setAttribute("role", "tab");
      button.setAttribute("aria-controls", panelID);
      button.setAttribute("aria-selected", String(index === 0));
      button.tabIndex = index === 0 ? 0 : -1;
      button.textContent = label;
      list.append(button);

      item.classList.remove(
        "markdown-container",
        "markdown-container-tab-item",
      );
      item.classList.add("markdown-tab-panel");
      item.id = panelID;
      item.setAttribute("role", "tabpanel");
      item.setAttribute("aria-labelledby", tabID);
      item.tabIndex = 0;
      item.hidden = index !== 0;
      panels.append(item);
    });

    group.append(list, panels);
  }
  return document.body.innerHTML;
}

function tabButtons(button: HTMLButtonElement):
  | {
      buttons: HTMLButtonElement[];
      panels: HTMLElement[];
    }
  | undefined {
  const group = button.closest<HTMLElement>(".markdown-tabs");
  const list = button.closest<HTMLElement>(".markdown-tabs-list");
  if (
    !group ||
    !contentRoot.value?.contains(group) ||
    list?.parentElement !== group
  )
    return;
  return {
    buttons: Array.from(
      list.querySelectorAll<HTMLButtonElement>('[role="tab"]'),
    ),
    panels: Array.from(
      group.querySelectorAll<HTMLElement>('[role="tabpanel"]'),
      // 嵌套分页的显示状态只由它自己的标签栏控制。
    ).filter((panel) => panel.closest(".markdown-tabs") === group),
  };
}

function activateTab(button: HTMLButtonElement, focus = false): void {
  const tabs = tabButtons(button);
  if (!tabs) return;
  for (const tab of tabs.buttons) {
    const selected = tab === button;
    tab.setAttribute("aria-selected", String(selected));
    tab.tabIndex = selected ? 0 : -1;
    const panelID = tab.getAttribute("aria-controls");
    const panel = tabs.panels.find((item) => item.id === panelID);
    if (panel) panel.hidden = !selected;
  }
  if (focus) button.focus();
}

function handleTabClick(event: MouseEvent): void {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const button = target.closest<HTMLButtonElement>(
    ".markdown-tabs-list button[role='tab']",
  );
  if (button) activateTab(button);
}

function handleTabKeydown(event: KeyboardEvent): void {
  const target = event.target;
  if (!(target instanceof Element)) return;
  const button = target.closest<HTMLButtonElement>(
    ".markdown-tabs-list button[role='tab']",
  );
  if (!button) return;
  const tabs = tabButtons(button);
  if (!tabs || tabs.buttons.length < 2) return;

  const index = tabs.buttons.indexOf(button);
  let nextIndex: number;
  switch (event.key) {
    case "ArrowRight":
      nextIndex = (index + 1) % tabs.buttons.length;
      break;
    case "ArrowLeft":
      nextIndex = (index - 1 + tabs.buttons.length) % tabs.buttons.length;
      break;
    case "Home":
      nextIndex = 0;
      break;
    case "End":
      nextIndex = tabs.buttons.length - 1;
      break;
    default:
      return;
  }
  event.preventDefault();
  activateTab(tabs.buttons[nextIndex], true);
}
</script>

<template>
  <div
    ref="contentRoot"
    class="markdown-content"
    v-html="html"
    @click="handleTabClick"
    @keydown="handleTabKeydown"
  ></div>
</template>

<style scoped lang="scss">
.markdown-content {
  min-width: 0;
  line-height: 1.8;
  overflow-wrap: anywhere;
}
.markdown-content :deep(> :first-child) {
  margin-top: 0;
}
.markdown-content :deep(> :last-child) {
  margin-bottom: 0;
}
.markdown-content :deep(h1) {
  font-size: 26px;
}
.markdown-content :deep(h2) {
  font-size: 22px;
}
.markdown-content :deep(h3) {
  font-size: 18px;
}
.markdown-content :deep(h1),
.markdown-content :deep(h2),
.markdown-content :deep(h3) {
  line-height: 1.4;
  margin: 24px 0 12px;
}
.markdown-content :deep(p),
.markdown-content :deep(ul),
.markdown-content :deep(ol) {
  margin: 12px 0;
}
.markdown-content :deep(img),
.markdown-content :deep(video) {
  display: block;
  max-width: 100%;
  height: auto;
  margin: 16px 0;
  border-radius: 8px;
}
.markdown-content :deep(video) {
  width: 100%;
  max-height: 65vh;
  background: #111827;
}
.markdown-content :deep(a) {
  color: #18a058;
  text-decoration: underline;
}
.markdown-content :deep(blockquote) {
  margin: 16px 0;
  padding: 4px 16px;
  border-left: 3px solid #e5e7eb;
  color: #6b7280;
}
.markdown-content :deep(pre) {
  max-width: 100%;
  overflow-x: auto;
  background: #f6f7f9;
  padding: 14px;
  border-radius: 8px;
}
.markdown-content :deep(code) {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
  background: #f6f7f9;
  padding: 2px 4px;
  border-radius: 4px;
}
.markdown-content :deep(pre code) {
  padding: 0;
  background: transparent;
}
.markdown-content :deep(table) {
  display: block;
  max-width: 100%;
  overflow-x: auto;
  border-collapse: collapse;
  margin: 16px 0;
}
.markdown-content :deep(th),
.markdown-content :deep(td) {
  border: 1px solid #e5e7eb;
  padding: 8px 12px;
}
.markdown-content :deep(th) {
  background: #f6f7f9;
  text-align: left;
}
.markdown-content :deep(hr) {
  border: 0;
  border-top: 1px solid #e5e7eb;
  margin: 20px 0;
}
.markdown-content :deep(.markdown-container) {
  margin: 16px 0;
  padding: 14px 16px;
  border-left: 3px solid #9ca3af;
  border-radius: 8px;
  background: #f6f7f9;
  min-width: 0;
}
.markdown-content :deep(.markdown-container-tip),
.markdown-content :deep(.markdown-container-success) {
  border-color: #18a058;
  background: #eef8f1;
}
.markdown-content :deep(.markdown-container-info),
.markdown-content :deep(.markdown-container-note) {
  border-color: #2080f0;
  background: #eff6ff;
}
.markdown-content :deep(.markdown-container-warning) {
  border-color: #f0a020;
  background: #fff8eb;
}
.markdown-content :deep(.markdown-container-danger),
.markdown-content :deep(.markdown-container-error) {
  border-color: #d03050;
  background: #fff1f3;
}
.markdown-content :deep(.markdown-container > :first-child) {
  margin-top: 0;
}
.markdown-content :deep(.markdown-container > :last-child) {
  margin-bottom: 0;
}
.markdown-content :deep(.markdown-container-title) {
  font-weight: 600;
}
.markdown-content :deep(summary) {
  cursor: pointer;
  font-weight: 600;
  border-radius: 4px;
}
.markdown-content :deep(summary:hover) {
  color: #18a058;
}
.markdown-content :deep(summary:active) {
  color: #0c7a43;
}
.markdown-content :deep(.markdown-tabs) {
  min-width: 0;
  margin: 16px 0;
  overflow: hidden;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
}
.markdown-content :deep(.markdown-tabs-list) {
  display: flex;
  overflow-x: auto;
  border-bottom: 1px solid #e5e7eb;
  background: #f9fafb;
}
.markdown-content :deep(.markdown-tab) {
  flex: 0 0 auto;
  margin-bottom: -1px;
  padding: 10px 14px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: #6b7280;
  cursor: pointer;
  font: inherit;
  white-space: nowrap;
}
.markdown-content :deep(.markdown-tab[aria-selected="true"]) {
  border-bottom-color: #18a058;
  color: #18a058;
  font-weight: 600;
}
.markdown-content :deep(.markdown-tab:focus-visible) {
  outline: 2px solid #18a058;
  outline-offset: -2px;
}
.markdown-content :deep(.markdown-tabs-panels) {
  min-width: 0;
  padding: 16px;
}
.markdown-content :deep(.markdown-tab-panel[hidden]) {
  display: none;
}
.markdown-content :deep(.markdown-tab-panel > :first-child) {
  margin-top: 0;
}
.markdown-content :deep(.markdown-tab-panel > :last-child) {
  margin-bottom: 0;
}
</style>
