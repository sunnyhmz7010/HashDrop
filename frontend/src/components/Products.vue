<script setup lang="ts">
import { t } from "../i18n";

import { computed, onMounted, reactive, ref } from "vue";
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NSelect,
  NSpin,
  NSwitch,
  useDialog,
  useMessage,
} from "naive-ui";
import FormDialog from "./FormDialog.vue";
import MarkdownEditor from "./MarkdownEditor.vue";
import {
  api,
  errorText,
  kindNames,
  money,
  priceText,
  toMinor,
  type Inventory,
  type Product,
} from "../api";
const message = useMessage(),
  dialog = useDialog(),
  products = ref<Product[]>([]),
  inventories = ref<Inventory[]>([]),
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
  inventory_id: null as string | null,
  active: false,
  hidden: false,
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
const inventoryOptions = computed(() =>
  inventories.value.map((item) => ({
    value: item.id,
    label: `${item.name} · ${kindNames.value[item.kind]} · ${item.id.slice(0, 6)}`,
  })),
);
let loadGeneration = 0;
async function load() {
  const current = ++loadGeneration;
  loading.value = true;
  error.value = "";
  try {
    const [items, pools] = await Promise.all([
      api<Product[]>("/admin/products"),
      api<Inventory[]>("/admin/inventories"),
    ]);
    if (current === loadGeneration) {
      products.value = items;
      inventories.value = pools;
    }
  } catch (e) {
    if (current === loadGeneration) error.value = errorText(e);
  } finally {
    if (current === loadGeneration) loading.value = false;
  }
}
onMounted(load);
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
          inventory_id: product.inventory_id || null,
        }
      : {
          id: "",
          name: "",
          description: "",
          details: "",
          price: "",
          currency: "CNY",
          inventory_id: null,
          active: false,
          hidden: false,
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
        inventory_id: model.inventory_id ?? "",
        active: model.active,
        hidden: model.hidden,
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
  <n-spin :show="loading"
    ><div v-if="products.length" class="panel table-panel">
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ t("common.product") }}</th>
              <th>{{ t("inventory.association") }}</th>
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
              <td class="name-cell">
                {{ p.inventory_name || t("inventory.unassigned") }}
                <span v-if="p.kind" class="muted">
                  · {{ kindNames[p.kind] }}</span
                >
              </td>
              <td>{{ money(p.price, p.currency) }}</td>
              <td>
                {{
                  !p.inventory_id
                    ? "—"
                    : ["card", "account"].includes(p.kind)
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
      <n-empty :description="t('product.empty')" /></div
  ></n-spin>
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
          <label class="field-label" for="product-inventory">{{
            t("inventory.association")
          }}</label
          ><n-select
            id="product-inventory"
            v-model:value="model.inventory_id"
            :options="inventoryOptions"
            :placeholder="t('inventory.select')"
            filterable
            clearable
            :disabled="saving"
          />
          <p class="helper">{{ t("inventory.association_help") }}</p>
          <n-alert v-if="!inventories.length" type="info">{{
            t("inventory.create_first")
          }}</n-alert>
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
