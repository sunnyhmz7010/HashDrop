<!-- 基于 HashPay OrderModal 的 NModal/NCard 结构修改，统一正文滚动与固定页脚。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { NCard, NModal } from "naive-ui";
const show = defineModel<boolean>("show", { required: true });
withDefaults(defineProps<{ title: string; busy?: boolean }>(), { busy: false });
</script>

<template>
  <n-modal v-model:show="show" :mask-closable="!busy" :close-on-esc="!busy">
    <n-card
      class="form-modal order-modal"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
      :title="title"
      :closable="!busy"
      :content-style="{ minHeight: '0', overflow: 'auto' }"
      :header-style="{ flexShrink: '0' }"
      :footer-style="{ flexShrink: '0' }"
      @close="show = false"
    >
      <slot />
      <template v-if="$slots.footer" #footer
        ><div class="form-actions"><slot name="footer" /></div
      ></template>
    </n-card>
  </n-modal>
</template>

<style scoped>
.form-modal {
  max-height: calc(100dvh - 28px);
  margin: 14px auto;
}
.form-actions {
  justify-content: flex-end;
  flex-wrap: wrap;
}
</style>
