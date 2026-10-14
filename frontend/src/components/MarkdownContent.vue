<script setup lang="ts">
import { computed } from "vue";
import createDOMPurify from "dompurify";
import { Marked } from "marked";

const props = defineProps<{ content: string }>();
const parser = new Marked({
  gfm: true,
  breaks: true,
  renderer: {
    image({ href, text }) {
      // 视频直链沿用图片的 Markdown 写法，查询参数不会影响文件类型识别。
      let url: URL;
      try {
        url = new URL(href, location.origin);
      } catch {
        return false;
      }
      if (!/\.(mp4|webm|ogv)$/i.test(url.pathname)) return false;
      const escape = (value: string) =>
        value.replace(
          /[&<>"']/g,
          (char) =>
            ({
              "&": "&amp;",
              "<": "&lt;",
              ">": "&gt;",
              '"': "&quot;",
              "'": "&#39;",
            })[char]!,
        );
      return `<video src="${escape(href)}" aria-label="${escape(text)}" controls preload="metadata"></video>`;
    },
  },
});
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
  purifier.sanitize(parser.parse(props.content, { async: false }), {
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
    ],
    ALLOW_DATA_ATTR: false,
    ALLOW_ARIA_ATTR: false,
  }),
);
</script>

<template><div class="markdown-content" v-html="html"></div></template>

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
</style>
