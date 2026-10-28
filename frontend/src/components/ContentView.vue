<script setup lang="ts">
import { NButton, useMessage } from "naive-ui";
import { copyText, type ContentDocument } from "../api";
import { safeContentLink } from "../content";
import { t } from "../i18n";
import MarkdownContent from "./MarkdownContent.vue";

defineProps<{ content: ContentDocument }>();
const message = useMessage();
async function copy(text: string) {
  try {
    await copyText(text);
    message.success(t("common.copied"));
  } catch {
    message.warning(t("common.copy_fallback"));
  }
}
</script>

<template>
  <div class="content-document">
    <section
      v-for="block in content.blocks"
      :key="block.id"
      class="content-block"
      :data-content-type="block.type"
    >
      <template v-if="block.type === 'text'">
        <div class="content-text-tools">
          <n-button text size="small" @click="copy(block.text ?? '')">{{
            t("content.copy_text")
          }}</n-button>
        </div>
        <MarkdownContent :content="block.text ?? ''" />
      </template>
      <a
        v-else-if="block.type === 'link' && safeContentLink(block.url)"
        :href="safeContentLink(block.url)"
        target="_blank"
        rel="noopener noreferrer nofollow"
        >{{ block.label || block.url }}</a
      >
      <div v-else-if="block.type === 'stock'" class="content-stock-placeholder">
        {{
          t("content.stock_preview", {
            name: block.label,
            quantity: block.quantity ?? 1,
          })
        }}
      </div>
    </section>
  </div>
</template>

<style scoped>
.content-document {
  display: grid;
  gap: 16px;
  min-width: 0;
  white-space: normal;
}
.content-block {
  min-width: 0;
  overflow-wrap: anywhere;
}
.content-text-tools {
  display: flex;
  justify-content: flex-end;
}
.content-stock-placeholder {
  border: 1px dashed #d1d5db;
  border-radius: 8px;
  padding: 12px;
  color: #6b7280;
}
</style>
