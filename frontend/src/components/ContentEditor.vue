<script setup lang="ts">
import { NButton, NInput, NInputNumber, NTabPane, NTabs } from "naive-ui";
import type { ContentBlock, ContentDocument } from "../api";
import { blockID } from "../content";
import { t } from "../i18n";
import MarkdownEditor from "./MarkdownEditor.vue";
import ContentView from "./ContentView.vue";

const content = defineModel<ContentDocument>({ required: true });
const props = withDefaults(
  defineProps<{ allowStock?: boolean; disabled?: boolean }>(),
  { allowStock: false, disabled: false },
);
function add(type: ContentBlock["type"]) {
  if (props.disabled) return;
  const block: ContentBlock = { id: blockID(), type };
  if (type === "text") block.text = "";
  if (type === "link") {
    block.url = "";
    block.label = "";
  }
  if (type === "stock") {
    block.label = t("content.stock");
    block.quantity = 1;
  }
  content.value = { blocks: [...content.value.blocks, block] };
}
function update(id: string, patch: Partial<ContentBlock>) {
  content.value = {
    blocks: content.value.blocks.map((block) =>
      block.id === id ? { ...block, ...patch } : block,
    ),
  };
}
function move(index: number, offset: number) {
  const blocks = [...content.value.blocks];
  const next = index + offset;
  if (props.disabled || next < 0 || next >= blocks.length) return;
  [blocks[index], blocks[next]] = [blocks[next]!, blocks[index]!];
  content.value = { blocks };
}
function remove(id: string) {
  content.value = {
    blocks: content.value.blocks.filter((block) => block.id !== id),
  };
}
</script>

<template>
  <div class="content-editor">
    <n-tabs type="line">
      <n-tab-pane name="edit" :tab="t('content.edit')" display-directive="show">
        <div class="content-editor-blocks">
          <section
            v-for="(block, index) in content.blocks"
            :key="block.id"
            class="content-editor-block"
            :data-content-type="block.type"
          >
            <div class="content-editor-heading">
              <strong>{{ t(`content.${block.type}`) }}</strong>
              <div class="row-actions">
                <n-button
                  size="tiny"
                  :disabled="disabled || index === 0"
                  :aria-label="t('content.move_up')"
                  @click="move(index, -1)"
                  >↑</n-button
                >
                <n-button
                  size="tiny"
                  :disabled="disabled || index === content.blocks.length - 1"
                  :aria-label="t('content.move_down')"
                  @click="move(index, 1)"
                  >↓</n-button
                >
                <n-button
                  size="tiny"
                  :disabled="disabled"
                  @click="remove(block.id)"
                  >{{ t("common.remove") }}</n-button
                >
              </div>
            </div>
            <MarkdownEditor
              v-if="block.type === 'text'"
              :id="`content-${block.id}`"
              :input-id="`content-text-${block.id}`"
              :model-value="block.text ?? ''"
              :maxlength="65536"
              :disabled="disabled"
              @update:model-value="update(block.id, { text: $event })"
            />
            <template v-else-if="block.type === 'stock'">
              <label :for="`stock-label-${block.id}`">{{
                t("content.stock_label")
              }}</label>
              <n-input
                :value="block.label"
                :disabled="disabled"
                :maxlength="120"
                :input-props="{ id: `stock-label-${block.id}` }"
                @update:value="update(block.id, { label: $event })"
              />
              <label :for="`stock-count-${block.id}`">{{
                t("content.stock_quantity")
              }}</label>
              <n-input-number
                :value="block.quantity ?? 1"
                :disabled="disabled"
                :min="1"
                :max="100"
                :precision="0"
                :input-props="{ id: `stock-count-${block.id}` }"
                @update:value="update(block.id, { quantity: $event ?? 1 })"
              />
            </template>
            <template v-else>
              <label :for="`content-label-${block.id}`">{{
                t("content.label")
              }}</label>
              <n-input
                :value="block.label ?? ''"
                :disabled="disabled"
                :maxlength="240"
                :input-props="{ id: `content-label-${block.id}` }"
                @update:value="update(block.id, { label: $event })"
              />
              <template v-if="block.type === 'link'">
                <label :for="`content-url-${block.id}`">{{
                  t("content.url")
                }}</label>
                <n-input
                  :value="block.url ?? ''"
                  :disabled="disabled"
                  :maxlength="4096"
                  placeholder="https://"
                  :input-props="{ id: `content-url-${block.id}`, type: 'url' }"
                  @update:value="update(block.id, { url: $event })"
                />
              </template>
            </template>
          </section>
          <p v-if="!content.blocks.length" class="muted">
            {{ t("content.empty") }}
          </p>
        </div>
        <div class="row-actions content-add-actions">
          <n-button :disabled="disabled" @click="add('text')">{{
            t("content.add_text")
          }}</n-button>
          <n-button :disabled="disabled" @click="add('link')">{{
            t("content.add_link")
          }}</n-button>
          <n-button
            v-if="allowStock"
            :disabled="disabled"
            @click="add('stock')"
            >{{ t("content.add_stock") }}</n-button
          >
        </div>
      </n-tab-pane>
      <n-tab-pane
        name="preview"
        :tab="t('content.preview')"
        display-directive="if"
      >
        <ContentView :content="content" />
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<style scoped>
.content-editor,
.content-editor-blocks {
  min-width: 0;
}
.content-editor-blocks {
  display: grid;
  gap: 12px;
}
.content-editor-block {
  display: grid;
  gap: 8px;
  border: 1px solid #e5e7eb;
  padding: 12px;
  border-radius: 8px;
  min-width: 0;
}
.content-editor-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
}
.content-add-actions {
  margin-top: 12px;
  flex-wrap: wrap;
}
</style>
