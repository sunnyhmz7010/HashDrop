<script setup lang="ts">
import { computed, onUnmounted, ref } from "vue";
import {
  NAlert,
  NButton,
  NInput,
  NInputNumber,
  NTabPane,
  NTabs,
} from "naive-ui";
import {
  api,
  errorText,
  type Asset,
  type ContentBlock,
  type ContentDocument,
} from "../api";
import { blockID } from "../content";
import { t } from "../i18n";
import MarkdownEditor from "./MarkdownEditor.vue";
import ContentView from "./ContentView.vue";

const content = defineModel<ContentDocument>({ required: true });
const props = withDefaults(
  defineProps<{ allowStock?: boolean; disabled?: boolean }>(),
  { allowStock: false, disabled: false },
);
const emit = defineEmits<{ busy: [value: boolean] }>();
const uploading = ref(false),
  error = ref("");
const locked = computed(() => props.disabled || uploading.value);
const uploadType = ref<"image" | "file">("file");
const fileInput = ref<HTMLInputElement>();
let controller: AbortController | undefined;
function add(type: ContentBlock["type"]) {
  if (locked.value) return;
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
  if (locked.value || next < 0 || next >= blocks.length) return;
  [blocks[index], blocks[next]] = [blocks[next]!, blocks[index]!];
  content.value = { blocks };
}
function remove(id: string) {
  content.value = {
    blocks: content.value.blocks.filter((block) => block.id !== id),
  };
}
function choose(type: "image" | "file") {
  uploadType.value = type;
  if (fileInput.value) {
    fileInput.value.accept =
      type === "image" ? "image/png,image/jpeg,image/gif,image/webp" : "";
    fileInput.value.click();
  }
}
async function upload(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file || locked.value) return;
  error.value = "";
  if (file.size > 100 * 1024 * 1024) {
    error.value = t("content.file_limit");
    return;
  }
  if (
    uploadType.value === "image" &&
    !["image/png", "image/jpeg", "image/gif", "image/webp"].includes(file.type)
  ) {
    error.value = t("content.image_invalid");
    return;
  }
  const type = uploadType.value;
  uploading.value = true;
  emit("busy", true);
  controller = new AbortController();
  try {
    const form = new FormData();
    form.append("file", file);
    const asset = await api<Asset>("/admin/assets", {
      method: "POST",
      body: form,
      signal: controller.signal,
    });
    if (controller.signal.aborted) return;
    content.value = {
      blocks: [
        ...content.value.blocks,
        {
          id: blockID(),
          type,
          label: Array.from(asset.name).slice(0, 240).join(""),
          asset_id: asset.id,
        },
      ],
    };
  } catch (cause) {
    if (!controller.signal.aborted) error.value = errorText(cause);
  } finally {
    uploading.value = false;
    emit("busy", false);
  }
}
onUnmounted(() => {
  controller?.abort();
  emit("busy", false);
});
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
                  :disabled="locked || index === 0"
                  :aria-label="t('content.move_up')"
                  @click="move(index, -1)"
                  >↑</n-button
                >
                <n-button
                  size="tiny"
                  :disabled="locked || index === content.blocks.length - 1"
                  :aria-label="t('content.move_down')"
                  @click="move(index, 1)"
                  >↓</n-button
                >
                <n-button
                  size="tiny"
                  :disabled="locked"
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
              :disabled="locked"
              @update:model-value="update(block.id, { text: $event })"
            />
            <template v-else-if="block.type === 'stock'">
              <label :for="`stock-label-${block.id}`">{{
                t("content.stock_label")
              }}</label>
              <n-input
                :value="block.label"
                :disabled="locked"
                :maxlength="120"
                :input-props="{ id: `stock-label-${block.id}` }"
                @update:value="update(block.id, { label: $event })"
              />
              <label :for="`stock-count-${block.id}`">{{
                t("content.stock_quantity")
              }}</label>
              <n-input-number
                :value="block.quantity ?? 1"
                :disabled="locked"
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
                :disabled="locked"
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
                  :disabled="locked"
                  :maxlength="4096"
                  placeholder="https://"
                  :input-props="{ id: `content-url-${block.id}`, type: 'url' }"
                  @update:value="update(block.id, { url: $event })"
                />
              </template>
              <small v-else class="muted">{{ t("content.uploaded") }}</small>
            </template>
          </section>
          <p v-if="!content.blocks.length" class="muted">
            {{ t("content.empty") }}
          </p>
        </div>
        <div class="row-actions content-add-actions">
          <n-button :disabled="locked" @click="add('text')">{{
            t("content.add_text")
          }}</n-button>
          <n-button :disabled="locked" @click="choose('image')">{{
            t("content.add_image")
          }}</n-button>
          <n-button :disabled="locked" @click="choose('file')">{{
            t("content.add_file")
          }}</n-button>
          <n-button :disabled="locked" @click="add('link')">{{
            t("content.add_link")
          }}</n-button>
          <n-button
            v-if="allowStock"
            :disabled="locked"
            @click="add('stock')"
            >{{ t("content.add_stock") }}</n-button
          >
        </div>
        <input
          ref="fileInput"
          type="file"
          class="content-file-input"
          :disabled="locked"
          :aria-label="t('content.upload')"
          @change="upload"
        />
        <p v-if="uploading" role="status">{{ t("content.uploading") }}</p>
        <n-alert v-if="error" type="error" class="space-top">{{
          error
        }}</n-alert>
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
.content-file-input {
  display: none;
}
</style>
