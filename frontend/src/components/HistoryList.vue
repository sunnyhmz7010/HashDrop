<script setup lang="ts" generic="T extends { id: string }">
import { NAlert, NButton, NEmpty, NPagination, NSpin } from "naive-ui";
import { t } from "../i18n";

defineProps<{
  title: string;
  items: T[];
  columns: string[];
  loading: boolean;
  error: string;
  emptyText: string;
  clearText: string;
  total: number;
  totalText: string;
  page: number;
}>();
defineEmits<{
  refresh: [];
  query: [];
  clear: [];
  "update:page": [page: number];
}>();
</script>

<template>
  <div class="grid history-list">
    <div class="section-title">
      <h2>{{ title }}</h2>
      <div class="row-actions">
        <n-button :loading="loading" @click="$emit('refresh')">{{
          t("common.refresh")
        }}</n-button>
        <n-button :disabled="loading || !total" @click="$emit('clear')">{{
          clearText
        }}</n-button>
        <n-button type="primary" @click="$emit('query')">{{
          t("common.query")
        }}</n-button>
      </div>
    </div>
    <slot name="notice" />
    <n-alert v-if="error" type="warning"
      >{{ error
      }}<n-button text @click="$emit('refresh')">{{
        t("common.retry")
      }}</n-button></n-alert
    >
    <div class="orders-workbench">
      <n-spin :show="loading">
        <div class="orders-table">
          <div class="orders-table-head order-row--plain">
            <span v-for="(column, index) in columns" :key="index">{{
              column
            }}</span>
          </div>
          <slot v-for="item in items" :key="item.id" name="row" :item="item" />
          <div
            v-if="!items.length && !loading && !error"
            class="orders-table-empty"
          >
            <n-empty :description="emptyText" />
          </div>
        </div>
      </n-spin>
      <div class="orders-pagination">
        <span>{{ totalText }}</span>
        <n-pagination
          :page="page"
          :page-size="20"
          :item-count="total"
          :page-slot="5"
          size="small"
          :disabled="loading"
          @update:page="(next) => $emit('update:page', next)"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.orders-workbench {
  grid-template-columns: minmax(0, 1fr);
}
.orders-workbench > * {
  min-width: 0;
}
.orders-pagination,
.section-title {
  flex-wrap: wrap;
}
.row-actions {
  margin-left: auto;
  justify-content: flex-end;
}
</style>
