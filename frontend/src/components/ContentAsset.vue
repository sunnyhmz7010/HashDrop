<script setup lang="ts">
import { onUnmounted, ref, watch } from "vue";
import { NAlert, NButton, NSkeleton } from "naive-ui";
import { assetBlob, errorText, type ContentBlock } from "../api";
import { t } from "../i18n";

const props = defineProps<{
  block: ContentBlock;
  assetBase: string;
  token?: string;
}>();
const url = ref(""),
  busy = ref(false),
  error = ref("");
let controller: AbortController | undefined;
let generation = 0;
function reset() {
  generation++;
  controller?.abort();
  if (url.value) URL.revokeObjectURL(url.value);
  url.value = "";
  busy.value = false;
}
async function load(download = false) {
  if (busy.value || !props.block.asset_id) return;
  const current = generation;
  controller = new AbortController();
  busy.value = true;
  error.value = "";
  try {
    const { blob, name } = await assetBlob(
      `${props.assetBase}/${encodeURIComponent(props.block.asset_id)}`,
      props.token,
      controller.signal,
    );
    if (current !== generation) return;
    if (
      !download &&
      !["image/png", "image/jpeg", "image/gif", "image/webp"].includes(
        blob.type,
      )
    )
      throw new Error(t("content.image_invalid"));
    if (url.value) URL.revokeObjectURL(url.value);
    url.value = URL.createObjectURL(blob);
    if (download) {
      const link = document.createElement("a");
      link.href = url.value;
      link.download = name;
      link.click();
    }
  } catch (cause) {
    if (current === generation) error.value = errorText(cause);
  } finally {
    if (current === generation) busy.value = false;
  }
}
watch(
  () => [props.block.asset_id, props.assetBase, props.token],
  () => {
    reset();
    error.value = "";
    if (props.block.type === "image") void load();
  },
  { immediate: true },
);
onUnmounted(reset);
</script>

<template>
  <div class="content-asset">
    <template v-if="block.type === 'image'">
      <n-skeleton v-if="busy" height="160px" />
      <img
        v-else-if="url"
        :src="url"
        :alt="block.label || t('content.image')"
      />
      <small v-if="block.label" class="muted">{{ block.label }}</small>
    </template>
    <n-button v-else :loading="busy" @click="load(true)"
      >{{ t("content.download") }} ·
      {{ block.label || t("content.file") }}</n-button
    >
    <n-alert v-if="error" type="error">
      {{ error }}
      <n-button text :loading="busy" @click="load(block.type !== 'image')">{{
        t("common.retry")
      }}</n-button>
    </n-alert>
  </div>
</template>

<style scoped>
.content-asset {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.content-asset img {
  display: block;
  max-width: 100%;
  max-height: 640px;
  width: auto;
  height: auto;
  object-fit: contain;
  border-radius: 8px;
}
.content-asset :deep(.n-button) {
  max-width: 100%;
  height: auto;
  min-height: 34px;
  white-space: normal;
  padding: 8px 14px;
}
.content-asset :deep(.n-button__content) {
  overflow-wrap: anywhere;
  white-space: normal;
}
</style>
