<script setup lang="ts">
import { t } from "../i18n";

import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NInputNumber,
  NSelect,
  NSkeleton,
  NSwitch,
  useDialog,
  useMessage,
} from "naive-ui";
import FormDialog from "./FormDialog.vue";
import MarkdownEditor from "./MarkdownEditor.vue";
import ContentEditor from "./ContentEditor.vue";
import { cloneContent, stockPlan, hasContent } from "../content";
import {
  api,
  errorText,
  money,
  priceText,
  toMinor,
  type Product,
} from "../api";
const message = useMessage(),
  dialog = useDialog(),
  products = ref<Product[]>([]),
  loading = ref(false),
  loaded = ref(false),
  error = ref(""),
  editor = ref(false),
  saving = ref(false),
  deleting = ref(""),
  formError = ref("");
const contentUploading = ref(false);
const model = reactive({
  id: "",
  name: "",
  description: "",
  details: "",
  price: "",
  currency: "CNY",
  inventory_strategy: "oldest" as Product["inventory_strategy"],
  active: false,
  hidden: false,
  delivery_note: "",
  delivery_plan: stockPlan(t("content.stock")),
  min_quantity: 1,
  max_quantity: 100,
});
const currencies = [
  "CNY",
  "USD",
  "EUR",
  "GBP",
  "HKD",
  "JPY",
  "USDT",
  "USDC",
].map((value) => ({ label: value, value }));
const strategyOptions = computed(() =>
  (["random", "oldest", "newest"] as const).map((value) => ({
    value,
    label: t(`inventory.strategy_${value}`),
  })),
);
let loadGeneration = 0;
let loadController: AbortController | undefined;
async function load() {
  const current = ++loadGeneration;
  loadController?.abort();
  loadController = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const items = await api<Product[]>("/admin/products", {
      signal: loadController.signal,
    });
    if (current === loadGeneration) {
      products.value = items;
      loaded.value = true;
    }
  } catch (e) {
    if (current === loadGeneration) error.value = errorText(e);
  } finally {
    if (current === loadGeneration) loading.value = false;
  }
}
onMounted(load);
onUnmounted(() => {
  loadGeneration++;
  loadController?.abort();
});
function edit(product?: Product) {
  Object.assign(
    model,
    product
      ? {
          id: product.id,
          name: product.name,
          description: product.description,
          currency: product.currency,
          active: product.active,
          hidden: product.hidden ?? false,
          details: product.details ?? "",
          price: priceText(product.price, product.currency),
          inventory_strategy: product.inventory_strategy,
          delivery_note: product.delivery_note,
          delivery_plan: cloneContent(
            product.delivery_plan ?? stockPlan(t("content.stock")),
          ),
          min_quantity: product.min_quantity,
          max_quantity: product.max_quantity,
        }
      : {
          id: "",
          name: "",
          description: "",
          details: "",
          price: "",
          currency: "CNY",
          inventory_strategy: "oldest",
          active: false,
          hidden: false,
          delivery_note: "",
          delivery_plan: stockPlan(t("content.stock")),
          min_quantity: 1,
          max_quantity: 100,
        },
  );
  formError.value = "";
  editor.value = true;
}
async function save() {
  if (
    saving.value ||
    contentUploading.value ||
    !hasContent(model.delivery_plan)
  )
    return;
  saving.value = true;
  formError.value = "";
  try {
    const saved = await api<Product>(
      `/admin/products${model.id ? `/${model.id}` : ""}`,
      {
        method: model.id ? "PUT" : "POST",
        body: {
          name: model.name,
          description: model.description,
          details: model.details,
          price: toMinor(model.price, model.currency),
          currency: model.currency,
          inventory_strategy: model.inventory_strategy,
          active: model.active,
          hidden: model.hidden,
          delivery_note: model.delivery_note,
          delivery_plan: model.delivery_plan,
          min_quantity: model.min_quantity,
          max_quantity: model.max_quantity,
        },
      },
    );
    // 保存响应已包含库存统计，直接提交服务端结果，避免再等一轮刷新。
    loadGeneration++;
    loadController?.abort();
    loading.value = false;
    loaded.value = true;
    error.value = "";
    products.value = model.id
      ? products.value.map((item) => (item.id === saved.id ? saved : item))
      : [saved, ...products.value];
    editor.value = false;
    message.success(t("product.saved"));
  } catch (e) {
    formError.value = errorText(e);
  } finally {
    saving.value = false;
  }
}
function removeProduct(product: Product) {
  if (deleting.value) return;
  const confirmation = dialog.warning({
    title: t("product.delete_title"),
    content: t("product.delete_warning", { name: product.name }),
    positiveText: t("common.delete"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      if (deleting.value) return false;
      deleting.value = product.id;
      confirmation.loading = true;
      confirmation.negativeButtonProps = { disabled: true };
      confirmation.closable = false;
      confirmation.maskClosable = false;
      confirmation.closeOnEsc = false;
      try {
        await api(`/admin/products/${product.id}`, { method: "DELETE" });
        products.value = products.value.filter(
          (item) => item.id !== product.id,
        );
        message.success(t("product.deleted"));
        await load();
        return true;
      } catch (e) {
        message.error(errorText(e));
        return false;
      } finally {
        deleting.value = "";
        confirmation.loading = false;
        confirmation.negativeButtonProps = { disabled: false };
        confirmation.closable = true;
        confirmation.maskClosable = true;
        confirmation.closeOnEsc = true;
      }
    },
  });
}
</script>
<template>
  <div class="section-title space-bottom">
    <h2>{{ t("nav.products") }}</h2>
    <div class="row-actions">
      <n-button :loading="loading" @click="load">{{
        t("common.refresh")
      }}</n-button>
      <n-button type="primary" :disabled="loading || !!error" @click="edit()">{{
        t("product.add")
      }}</n-button>
    </div>
  </div>
  <n-alert v-if="error" type="error" class="space-bottom"
    >{{ errorText(error) }}
    <n-button text @click="load">{{ t("common.retry") }}</n-button></n-alert
  >
  <div :aria-busy="loading">
    <div
      v-if="products.length || (loading && !loaded)"
      class="panel table-panel"
    >
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ t("common.product") }}</th>
              <th>{{ t("inventory.strategy") }}</th>
              <th>{{ t("common.price") }}</th>
              <th>{{ t("stock.summary") }}</th>
              <th>{{ t("common.status") }}</th>
              <th>{{ t("common.actions") }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-if="loading && !loaded">
              <tr v-for="row in 3" :key="row" aria-hidden="true">
                <td v-for="column in 6" :key="column"><n-skeleton text /></td>
              </tr>
            </template>
            <tr v-for="p in products" :key="p.id">
              <td class="name-cell">
                <router-link v-if="p.active" :to="`/products/${p.id}`">{{
                  p.name
                }}</router-link>
                <template v-else>{{ p.name }}</template>
              </td>
              <td class="name-cell">
                {{ t(`inventory.strategy_${p.inventory_strategy}`) }}
                <span class="muted">
                  ·
                  {{
                    t("inventory.bound_count", { count: p.inventory_count })
                  }}</span
                >
              </td>
              <td>{{ money(p.price, p.currency) }}</td>
              <td>
                {{
                  p.stock_unlimited
                    ? t("stock.not_consumed")
                    : `${p.available} / ${p.reserved} / ${p.sold}`
                }}
              </td>
              <td>
                {{ p.active ? t("product.active") : t("product.inactive") }}
                <span v-if="p.hidden" class="muted">
                  · {{ t("product.hidden") }}</span
                >
              </td>
              <td>
                <div class="row-actions">
                  <n-button
                    size="small"
                    :disabled="deleting === p.id"
                    @click="edit(p)"
                    >{{ t("common.edit") }}</n-button
                  >
                  <n-button
                    size="small"
                    type="error"
                    secondary
                    :loading="deleting === p.id"
                    @click="removeProduct(p)"
                    >{{ t("common.delete") }}</n-button
                  >
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <div v-else-if="loaded && !error" class="panel empty-panel">
      <n-empty :description="t('product.empty')" />
    </div>
  </div>
  <FormDialog
    v-model:show="editor"
    :title="model.id ? t('product.edit') : t('product.add')"
    :busy="saving || contentUploading"
    ><form id="product-form" @submit.prevent="save">
      <div class="form-grid two">
        <div class="wide">
          <label class="field-label" for="product-name">{{
            t("product.name")
          }}</label
          ><n-input
            id="product-name"
            v-model:value="model.name"
            :maxlength="120"
          />
        </div>
        <div class="wide">
          <h3>{{ t("content.plan") }}</h3>
          <p class="helper">{{ t("content.plan_help") }}</p>
          <ContentEditor
            v-model="model.delivery_plan"
            allow-stock
            :disabled="saving"
            @busy="contentUploading = $event"
          />
        </div>
        <div>
          <label class="field-label" for="product-inventory-strategy">{{
            t("inventory.strategy")
          }}</label
          ><n-select
            id="product-inventory-strategy"
            v-model:value="model.inventory_strategy"
            :options="strategyOptions"
            :disabled="saving"
          />
          <p class="helper">{{ t("inventory.strategy_help") }}</p>
        </div>
        <div>
          <label class="field-label" for="currency">{{
            t("common.currency")
          }}</label
          ><n-select
            id="currency"
            v-model:value="model.currency"
            :options="currencies"
          />
        </div>
        <div>
          <label class="field-label" for="price">{{ t("product.price") }}</label
          ><n-input
            id="price"
            v-model:value="model.price"
            :placeholder="t('product.price_placeholder')"
            :input-props="{ inputmode: 'decimal' }"
          />
        </div>
        <div>
          <label class="field-label" for="active">{{
            t("product.publication")
          }}</label>
          <div class="publication-state">
            <n-switch id="active" v-model:value="model.active" />
            <span class="muted">{{
              model.active ? t("product.publish") : t("product.unpublish")
            }}</span>
          </div>
        </div>
        <div class="wide">
          <label class="field-label" for="hidden">{{
            t("product.hidden")
          }}</label>
          <n-switch
            id="hidden"
            v-model:value="model.hidden"
            :aria-label="t('product.hidden')"
          />
          <p class="helper">{{ t("product.hidden_help") }}</p>
        </div>
        <div class="wide">
          <label class="field-label" for="product-delivery-note">{{
            t("product.delivery_note")
          }}</label>
          <n-input
            id="product-delivery-note"
            v-model:value="model.delivery_note"
            type="textarea"
            :maxlength="4000"
            :disabled="saving"
            :placeholder="t('product.delivery_note_help')"
          />
        </div>
        <div>
          <label class="field-label" for="product-min-quantity">{{
            t("product.min_quantity")
          }}</label>
          <n-input-number
            :value="model.min_quantity"
            :min="1"
            :max="model.max_quantity"
            :precision="0"
            :disabled="saving"
            :input-props="{ id: 'product-min-quantity' }"
            @update:value="(value) => (model.min_quantity = value ?? 1)"
          />
        </div>
        <div>
          <label class="field-label" for="product-max-quantity">{{
            t("product.max_quantity")
          }}</label>
          <n-input-number
            :value="model.max_quantity"
            :min="model.min_quantity"
            :max="100"
            :precision="0"
            :disabled="saving"
            :input-props="{ id: 'product-max-quantity' }"
            @update:value="(value) => (model.max_quantity = value ?? 100)"
          />
        </div>
        <div class="wide">
          <label class="field-label" for="description">{{
            t("product.description")
          }}</label
          ><n-input
            id="description"
            v-model:value="model.description"
            type="textarea"
            :maxlength="4000"
          />
        </div>
        <div class="wide details-editor">
          <label class="field-label" for="product-details-input">{{
            t("product.details")
          }}</label>
          <MarkdownEditor
            id="details"
            input-id="product-details-input"
            v-model="model.details"
            :maxlength="65536"
          />
        </div>
      </div>
      <n-alert v-if="formError" type="error" class="space-top">{{
        errorText(formError)
      }}</n-alert>
    </form>
    <template #footer>
      <n-button
        :disabled="saving || contentUploading"
        @click="editor = false"
        >{{ t("common.cancel") }}</n-button
      ><n-button
        attr-type="submit"
        form="product-form"
        type="primary"
        :loading="saving"
        :disabled="contentUploading || !hasContent(model.delivery_plan)"
        >{{ t("product.save") }}</n-button
      >
    </template>
  </FormDialog>
</template>

<style scoped lang="scss">
.table-panel {
  padding: 0;
  overflow: hidden;
}

.table-scroll {
  overflow-x: auto;
}

.table-scroll table {
  width: 100%;
  border-collapse: collapse;
  text-align: left;
  font-size: 13px;
}

.table-scroll th {
  color: #6b7280;
  background: #f9fafb;
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
}

.table-scroll th,
.table-scroll td {
  padding: 9px 10px;
  border-bottom: 1px solid #eef0f3;
}

.table-scroll tr:last-child td {
  border-bottom: 0;
}

.table-scroll td {
  white-space: nowrap;
}

.table-scroll .name-cell {
  min-width: 8rem;
  max-width: 16rem;
  white-space: normal;
  overflow-wrap: anywhere;
}

.table-scroll .row-actions {
  flex-wrap: nowrap;
}

.name-cell a {
  color: #18a058;
}

.publication-state {
  display: flex;
  align-items: center;
  gap: 8px;
}

.form-grid .field-label {
  margin-bottom: 7px;
}

.form-grid .wide {
  grid-column: 1 / -1;
}
</style>
