<!-- 基于 HashPay LocaleSwitch.vue 修改，添加语言选择标签。
     Copyright 2026 TGDash Team. Apache-2.0，见 frontend/public/HashPay-LICENSE.txt。 -->
<script setup lang="ts">
import { NSelect } from "naive-ui";
import { useI18n } from "../i18n";
const { locale, localeOptions, setLocale, t } = useI18n();
</script>

<template>
  <n-select
    class="locale-switch"
    size="small"
    :options="localeOptions"
    :value="locale"
    :aria-label="t('locale.select')"
    @update:value="setLocale"
  />
</template>

<style scoped>
.locale-switch {
  flex: 0 0 9.5rem;
  width: 9.5rem;
}
</style>
