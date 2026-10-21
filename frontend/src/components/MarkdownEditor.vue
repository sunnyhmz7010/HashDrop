<script setup lang="ts">
import { NInput, NTabPane, NTabs } from "naive-ui";
import { t } from "../i18n";
import MarkdownContent from "./MarkdownContent.vue";

defineProps<{
  id: string;
  inputId: string;
  maxlength: number;
  disabled?: boolean;
}>();
const content = defineModel<string>({ required: true });
</script>

<template>
  <n-tabs type="line" animated>
    <n-tab-pane
      name="edit"
      :tab="t('product.edit_details')"
      display-directive="show"
    >
      <n-input
        :id="id"
        v-model:value="content"
        type="textarea"
        :maxlength="maxlength"
        :disabled="disabled"
        :input-props="{ id: inputId }"
      />
    </n-tab-pane>
    <n-tab-pane
      name="preview"
      :tab="t('product.preview_details')"
      display-directive="if"
    >
      <MarkdownContent :content="content" />
    </n-tab-pane>
  </n-tabs>
</template>
