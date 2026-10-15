<script setup lang="ts">
import { t } from "../i18n";

import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NSelect,
  NSpin,
  NSwitch,
  NTabs,
  NTabPane,
  useDialog,
  useMessage,
} from "naive-ui";
import FormDialog from "./FormDialog.vue";
import MarkdownContent from "./MarkdownContent.vue";
import {
  api,
  errorText,
  kindNames,
  money,
  priceText,
  toMinor,
  type Product,
} from "../api";
const message = useMessage(),
  dialog = useDialog(),
  products = ref<Product[]>([]),
  loading = ref(false),
  error = ref(""),
  editor = ref(false),
  saving = ref(false),
  deleting = ref(""),
  formError = ref("");
const model = reactive({
  id: "",
  name: "",
  description: "",
  details: "",
  price: "",
  currency: "CNY",
  kind: "card",
  active: false,
  hidden: false,
  static_content: "",
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
const kinds = computed(() =>
  Object.entries(kindNames.value).map(([value, label]) => ({
    value,
    label,
  })),
);
const stockNames = computed<Record<string, string>>(() => ({
  available: t("stock.available"),
  reserved: t("stock.reserved"),
  sold: t("stock.sold"),
}));
async function load() {
  loading.value = true;
  error.value = "";
  try {
    products.value = await api("/admin/products");
  } catch (e) {
    error.value = errorText(e);
  } finally {
    loading.value = false;
  }
}
onMounted(load);
function edit(product?: Product) {
  Object.assign(
    model,
    product
      ? {
          ...product,
          hidden: product.hidden ?? false,
          details: product.details ?? "",
          price: priceText(product.price, product.currency),
          static_content: "",
        }
      : {
          id: "",
          name: "",
          description: "",
          details: "",
          price: "",
          currency: "CNY",
          kind: "card",
          active: false,
          hidden: false,
          static_content: "",
        },
  );
  formError.value = "";
  editor.value = true;
}
async function save() {
  if (saving.value) return;
  saving.value = true;
  formError.value = "";
  try {
    await api(`/admin/products${model.id ? `/${model.id}` : ""}`, {
      method: model.id ? "PUT" : "POST",
      body: {
        name: model.name,
        description: model.description,
        details: model.details,
        price: toMinor(model.price, model.currency),
        currency: model.currency,
        kind: model.kind,
        active: model.active,
        hidden: model.hidden,
        static_content: model.static_content,
      },
    });
    editor.value = false;
    message.success(t("product.saved"));
    await load();
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
const stockProduct = ref<Product>(),
  importing = ref(false),
  stockText = ref(""),
  stockError = ref(""),
  stockDialog = ref(false),
  stockLoading = ref(false),
  stockItems = ref<
    { id: string; status: string; order_id: string; created_at: number }[]
  >([]);
function importFor(p: Product) {
  stockProduct.value = p;
  stockText.value = "";
  stockError.value = "";
  importing.value = true;
}
async function importItems() {
  if (saving.value || !stockProduct.value) return;
  saving.value = true;
  stockError.value = "";
  try {
    const result = await api<{ added: number; duplicates: number }>(
      `/admin/products/${stockProduct.value.id}/stock`,
      { method: "POST", body: { content: stockText.value } },
    );
    importing.value = false;
    stockText.value = "";
    message.success(
      t("stock.imported", {
        added: result.added,
        duplicates: result.duplicates,
      }),
    );
    await load();
  } catch (e) {
    stockError.value = errorText(e);
  } finally {
    saving.value = false;
  }
}
let stockGeneration = 0;
let stockController: AbortController | undefined;
function cancelStock() {
  stockGeneration++;
  stockController?.abort();
  stockLoading.value = false;
}
watch(stockDialog, (value) => {
  if (!value) cancelStock();
});
onUnmounted(cancelStock);
async function viewStock(p: Product) {
  cancelStock();
  const current = stockGeneration;
  stockController = new AbortController();
  stockProduct.value = p;
  stockItems.value = [];
  stockError.value = "";
  stockDialog.value = true;
  stockLoading.value = true;
  try {
    const result = await api<typeof stockItems.value>(
      `/admin/products/${p.id}/stock`,
      { signal: stockController.signal },
    );
    if (current === stockGeneration) stockItems.value = result;
  } catch (e) {
    if (current === stockGeneration) stockError.value = errorText(e);
  } finally {
    if (current === stockGeneration) stockLoading.value = false;
  }
}
function removeStock(id: string) {
  const p = stockProduct.value;
  if (!p) return;
  dialog.warning({
    title: t("stock.delete_title"),
    content: t("stock.delete_warning"),
    positiveText: t("common.delete"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      try {
        await api(`/admin/products/${p.id}/stock/${id}`, { method: "DELETE" });
        await viewStock(p);
        await load();
      } catch (e) {
        message.error(errorText(e));
        return false;
      }
    },
  });
}
</script>
<template>
  <div class="section-title space-bottom">
    <h2>{{ t("nav.products") }}</h2>
    <n-button type="primary" @click="edit()">{{ t("product.add") }}</n-button>
  </div>
  <n-alert v-if="error" type="error" class="space-bottom"
    >{{ errorText(error) }}
    <n-button text @click="load">{{ t("common.retry") }}</n-button></n-alert
  >
  <n-spin :show="loading"
    ><div v-if="products.length" class="panel table-panel">
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ t("common.product") }}</th>
              <th>{{ t("common.type") }}</th>
              <th>{{ t("common.price") }}</th>
              <th>{{ t("stock.summary") }}</th>
              <th>{{ t("common.status") }}</th>
              <th>{{ t("common.actions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in products" :key="p.id">
              <td class="name-cell">
                <router-link v-if="p.active" :to="`/products/${p.id}`">{{
                  p.name
                }}</router-link>
                <template v-else>{{ p.name }}</template>
              </td>
              <td>{{ kindNames[p.kind] }}</td>
              <td>{{ money(p.price, p.currency) }}</td>
              <td>
                {{
                  ["card", "account"].includes(p.kind)
                    ? `${p.available} / ${p.reserved} / ${p.sold}`
                    : t("stock.not_consumed")
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
                  ><template v-if="['card', 'account'].includes(p.kind)"
                    ><n-button
                      size="small"
                      :disabled="deleting === p.id"
                      @click="importFor(p)"
                      >{{ t("common.import") }}</n-button
                    ><n-button
                      size="small"
                      :disabled="deleting === p.id"
                      @click="viewStock(p)"
                      >{{ t("stock.label") }}</n-button
                    ></template
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
    <div v-else-if="!loading && !error" class="panel empty-panel">
      <n-empty :description="t('product.empty')"
        ><template #extra
          ><n-button @click="edit()">{{ t("product.add") }}</n-button></template
        ></n-empty
      >
    </div></n-spin
  >
  <FormDialog
    v-model:show="editor"
    :title="model.id ? t('product.edit') : t('product.add')"
    :busy="saving"
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
        <div>
          <label class="field-label" for="product-kind">{{
            t("product.delivery_type")
          }}</label
          ><n-select
            id="product-kind"
            v-model:value="model.kind"
            :options="kinds"
            :disabled="!!model.id"
          />
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
          <n-tabs type="line" animated>
            <n-tab-pane
              name="edit"
              :tab="t('product.edit_details')"
              display-directive="show"
            >
              <n-input
                id="details"
                v-model:value="model.details"
                type="textarea"
                :maxlength="65536"
                :input-props="{ id: 'product-details-input' }"
              />
            </n-tab-pane>
            <n-tab-pane
              name="preview"
              :tab="t('product.preview_details')"
              display-directive="if"
            >
              <MarkdownContent :content="model.details" />
            </n-tab-pane>
          </n-tabs>
        </div>
        <div v-if="model.kind === 'static'" class="wide">
          <label class="field-label" for="static-content">{{
            t("product.static_content")
          }}</label
          ><n-input
            id="static-content"
            v-model:value="model.static_content"
            type="textarea"
            :placeholder="
              model.id
                ? t('product.static_keep')
                : t('product.static_placeholder')
            "
          />
        </div>
      </div>
      <n-alert v-if="formError" type="error" class="space-top">{{
        errorText(formError)
      }}</n-alert>
    </form>
    <template #footer>
      <n-button :disabled="saving" @click="editor = false">{{
        t("common.cancel")
      }}</n-button
      ><n-button
        attr-type="submit"
        form="product-form"
        type="primary"
        :loading="saving"
        >{{ t("product.save") }}</n-button
      >
    </template>
  </FormDialog>
  <FormDialog
    v-model:show="importing"
    :title="t('stock.import_title', { name: stockProduct?.name ?? '' })"
    :busy="saving"
    ><p class="muted">{{ t("stock.import_help") }}</p>
    <n-input
      v-model:value="stockText"
      type="textarea"
      :autosize="{ minRows: 8, maxRows: 18 }"
      :placeholder="t('stock.placeholder')"
      :aria-label="t('stock.content')"
    /><n-alert v-if="stockError" type="error" class="space-top">{{
      errorText(stockError)
    }}</n-alert>
    <template #footer>
      <n-button :disabled="saving" @click="importing = false">{{
        t("common.cancel")
      }}</n-button
      ><n-button type="primary" :loading="saving" @click="importItems">{{
        t("stock.import")
      }}</n-button>
    </template></FormDialog
  >
  <FormDialog
    v-model:show="stockDialog"
    :title="t('stock.title', { name: stockProduct?.name ?? '' })"
    ><n-alert v-if="stockError" type="error"
      >{{ errorText(stockError) }}
      <n-button
        text
        :loading="stockLoading"
        @click="stockProduct && viewStock(stockProduct)"
        >{{ t("common.recheck") }}</n-button
      ></n-alert
    ><n-spin :show="stockLoading"
      ><div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ t("stock.id") }}</th>
              <th>{{ t("common.status") }}</th>
              <th>{{ t("common.actions") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in stockItems" :key="item.id">
              <td class="code-cell">{{ item.id }}</td>
              <td>{{ stockNames[item.status] }}</td>
              <td>
                <n-button
                  v-if="item.status === 'available'"
                  size="small"
                  type="error"
                  secondary
                  @click="removeStock(item.id)"
                  >{{ t("common.delete") }}</n-button
                >
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <n-empty
        v-if="!stockItems.length && !stockLoading && !stockError"
        :description="t('stock.empty')"
    /></n-spin>
    <p class="helper">{{ t("stock.list_help") }}</p></FormDialog
  >
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
