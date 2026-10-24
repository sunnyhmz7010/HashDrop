<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NSelect,
  NSkeleton,
  useDialog,
  useMessage,
} from "naive-ui";
import {
  api,
  errorText,
  kindNames,
  type Inventory,
  type Product,
} from "../api";
import { t } from "../i18n";
import FormDialog from "./FormDialog.vue";

const message = useMessage(),
  dialog = useDialog();
const inventories = ref<Inventory[]>([]),
  products = ref<Product[]>([]),
  loading = ref(false),
  loaded = ref(false),
  error = ref("");
const editor = ref(false),
  saving = ref(false),
  formError = ref("");
const deleting = ref("");
const model = reactive({
  id: "",
  name: "",
  kind: "card",
  static_content: "",
  product_ids: [] as string[],
});
const productOptions = computed(() =>
  products.value
    .filter((product) => product.delivery_mode === "automatic")
    .map((product) => ({
      value: product.id,
      label: `${product.name} · ${product.id.slice(0, 6)}`,
    })),
);
const kinds = computed(() =>
  ["card", "account", "static"].map((value) => ({
    value,
    label: kindNames.value[value]!,
  })),
);
const stockNames = computed<Record<string, string>>(() => ({
  available: t("stock.available"),
  reserved: t("stock.reserved"),
  sold: t("stock.sold"),
}));

let loadGeneration = 0;
let loadController: AbortController | undefined;
async function load() {
  const current = ++loadGeneration;
  loadController?.abort();
  loadController = new AbortController();
  loading.value = true;
  error.value = "";
  try {
    const [pools, items] = await Promise.all([
      api<Inventory[]>("/admin/inventories", { signal: loadController.signal }),
      api<Product[]>("/admin/products", { signal: loadController.signal }),
    ]);
    if (current === loadGeneration) {
      inventories.value = pools;
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

function edit(pool?: Inventory) {
  Object.assign(model, {
    id: pool?.id ?? "",
    name: pool?.name ?? "",
    kind: pool?.kind ?? "card",
    static_content: "",
    product_ids: [...(pool?.product_ids ?? [])],
  });
  formError.value = "";
  editor.value = true;
}
async function save() {
  if (saving.value) return;
  saving.value = true;
  formError.value = "";
  try {
    const saved = await api<Inventory>(
      `/admin/inventories${model.id ? `/${model.id}` : ""}`,
      {
        method: model.id ? "PUT" : "POST",
        body: {
          name: model.name,
          kind: model.kind,
          static_content: model.kind === "static" ? model.static_content : "",
          product_ids: model.product_ids,
        },
      },
    );
    loadGeneration++;
    loadController?.abort();
    loading.value = false;
    loaded.value = true;
    error.value = "";
    inventories.value = model.id
      ? inventories.value.map((item) => (item.id === saved.id ? saved : item))
      : [saved, ...inventories.value];
    editor.value = false;
    model.static_content = "";
    message.success(t("inventory.saved"));
  } catch (e) {
    formError.value = errorText(e);
  } finally {
    saving.value = false;
  }
}
function remove(pool: Inventory) {
  if (deleting.value) return;
  const confirmation = dialog.warning({
    title: t("inventory.delete_title"),
    content: t("inventory.delete_warning", { name: pool.name }),
    positiveText: t("common.delete"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      if (deleting.value) return false;
      deleting.value = pool.id;
      confirmation.loading = true;
      confirmation.negativeButtonProps = { disabled: true };
      confirmation.closable =
        confirmation.maskClosable =
        confirmation.closeOnEsc =
          false;
      try {
        await api(`/admin/inventories/${pool.id}`, { method: "DELETE" });
        message.success(t("inventory.deleted"));
        await load();
        return true;
      } catch (e) {
        message.error(errorText(e));
        return false;
      } finally {
        deleting.value = "";
        confirmation.loading = false;
        confirmation.negativeButtonProps = { disabled: false };
        confirmation.closable =
          confirmation.maskClosable =
          confirmation.closeOnEsc =
            true;
      }
    },
  });
}

const stockPool = ref<Inventory>(),
  importing = ref(false),
  importBusy = ref(false);
const stockText = ref(""),
  importError = ref("");
function importFor(pool: Inventory) {
  stockPool.value = pool;
  stockText.value = "";
  importError.value = "";
  importing.value = true;
}
async function importItems() {
  if (importBusy.value || !stockPool.value) return;
  const id = stockPool.value.id;
  importBusy.value = true;
  importError.value = "";
  try {
    const result = await api<{ added: number; duplicates: number }>(
      `/admin/inventories/${id}/stock`,
      {
        method: "POST",
        body: { content: stockText.value },
      },
    );
    importing.value = false;
    stockText.value = "";
    message.success(t("stock.imported", result));
    await load();
  } catch (e) {
    importError.value = errorText(e);
  } finally {
    importBusy.value = false;
  }
}

const stockDialog = ref(false),
  stockLoading = ref(false),
  stockError = ref("");
const stockDeleting = ref("");
const stockItems = ref<
  { id: string; status: string; order_id: string; created_at: number }[]
>([]);
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
async function viewStock(pool: Inventory) {
  cancelStock();
  const current = stockGeneration;
  stockController = new AbortController();
  if (!stockDialog.value || stockPool.value?.id !== pool.id)
    stockItems.value = [];
  stockPool.value = pool;
  stockError.value = "";
  stockDialog.value = true;
  stockLoading.value = true;
  try {
    const items = await api<typeof stockItems.value>(
      `/admin/inventories/${pool.id}/stock`,
      { signal: stockController.signal },
    );
    if (current === stockGeneration) stockItems.value = items;
  } catch (e) {
    if (current === stockGeneration) stockError.value = errorText(e);
  } finally {
    if (current === stockGeneration) stockLoading.value = false;
  }
}
function removeStock(id: string) {
  const pool = stockPool.value;
  if (!pool || stockDeleting.value) return;
  const confirmation = dialog.warning({
    title: t("stock.delete_title"),
    content: t("stock.delete_warning"),
    positiveText: t("common.delete"),
    negativeText: t("common.cancel"),
    onPositiveClick: async () => {
      if (stockDeleting.value) return false;
      stockDeleting.value = id;
      confirmation.loading = true;
      confirmation.negativeButtonProps = { disabled: true };
      confirmation.closable =
        confirmation.maskClosable =
        confirmation.closeOnEsc =
          false;
      try {
        await api(`/admin/inventories/${pool.id}/stock/${id}`, {
          method: "DELETE",
        });
        await viewStock(pool);
        await load();
        return true;
      } catch (e) {
        message.error(errorText(e));
        return false;
      } finally {
        stockDeleting.value = "";
        confirmation.loading = false;
        confirmation.negativeButtonProps = { disabled: false };
        confirmation.closable =
          confirmation.maskClosable =
          confirmation.closeOnEsc =
            true;
      }
    },
  });
}
</script>

<template>
  <div class="section-title space-bottom">
    <h2>{{ t("nav.inventories") }}</h2>
    <div class="row-actions">
      <n-button :loading="loading" @click="load">{{
        t("common.refresh")
      }}</n-button>
      <n-button type="primary" :disabled="loading || !!error" @click="edit()">{{
        t("inventory.add")
      }}</n-button>
    </div>
  </div>
  <n-alert v-if="error" type="error" class="space-bottom">
    {{ errorText(error) }}
    <n-button text @click="load">{{ t("common.retry") }}</n-button>
  </n-alert>
  <div :aria-busy="loading">
    <div
      v-if="inventories.length || (loading && !loaded)"
      class="panel table-panel"
    >
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ t("inventory.name") }}</th>
              <th>{{ t("inventory.delivery_type") }}</th>
              <th>{{ t("stock.summary") }}</th>
              <th>{{ t("inventory.products") }}</th>
              <th>{{ t("common.actions") }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-if="loading && !loaded">
              <tr v-for="row in 3" :key="row" aria-hidden="true">
                <td v-for="column in 5" :key="column"><n-skeleton text /></td>
              </tr>
            </template>
            <tr v-for="pool in inventories" :key="pool.id">
              <td class="name-cell">
                {{ pool.name
                }}<small class="muted code-cell">{{ pool.id }}</small>
              </td>
              <td>{{ kindNames[pool.kind] }}</td>
              <td>
                {{
                  pool.kind === "static"
                    ? pool.has_content
                      ? t("stock.not_consumed")
                      : t("inventory.content_missing")
                    : `${pool.available} / ${pool.reserved} / ${pool.sold}`
                }}
              </td>
              <td>
                {{ t("inventory.shared_count", { count: pool.product_count }) }}
                <small v-if="pool.product_ids.length" class="muted code-cell">{{
                  pool.product_ids
                    .map(
                      (id) =>
                        products.find((product) => product.id === id)?.name ??
                        id,
                    )
                    .join("、")
                }}</small>
              </td>
              <td>
                <div class="row-actions">
                  <n-button
                    size="small"
                    :disabled="!!deleting"
                    @click="edit(pool)"
                    >{{ t("common.edit") }}</n-button
                  >
                  <template v-if="pool.kind !== 'static'">
                    <n-button
                      size="small"
                      :disabled="!!deleting"
                      @click="importFor(pool)"
                      >{{ t("common.import") }}</n-button
                    >
                    <n-button
                      size="small"
                      :disabled="!!deleting"
                      @click="viewStock(pool)"
                      >{{ t("stock.label") }}</n-button
                    >
                  </template>
                  <span
                    :title="
                      pool.product_count || pool.reserved
                        ? t('inventory.delete_blocked')
                        : undefined
                    "
                  >
                    <n-button
                      size="small"
                      type="error"
                      secondary
                      :loading="deleting === pool.id"
                      :disabled="
                        !!pool.product_count ||
                        !!pool.reserved ||
                        (!!deleting && deleting !== pool.id)
                      "
                      @click="remove(pool)"
                      >{{ t("common.delete") }}</n-button
                    >
                  </span>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <div v-else-if="loaded && !error" class="panel empty-panel">
      <n-empty :description="t('inventory.empty')" />
    </div>
  </div>

  <FormDialog
    v-model:show="editor"
    :title="model.id ? t('inventory.edit') : t('inventory.add')"
    :busy="saving"
  >
    <form id="inventory-form" @submit.prevent="save">
      <div class="form-grid">
        <div>
          <label class="field-label" for="inventory-name">{{
            t("inventory.name")
          }}</label>
          <n-input
            id="inventory-name"
            v-model:value="model.name"
            :maxlength="120"
          />
        </div>
        <div>
          <label class="field-label" for="inventory-kind">{{
            t("inventory.delivery_type")
          }}</label>
          <n-select
            id="inventory-kind"
            v-model:value="model.kind"
            :options="kinds"
            :disabled="!!model.id"
          />
          <p class="helper">{{ t("inventory.kind_help") }}</p>
        </div>
        <div>
          <label class="field-label" for="inventory-products">{{
            t("inventory.products")
          }}</label>
          <n-select
            id="inventory-products"
            v-model:value="model.product_ids"
            :options="productOptions"
            :placeholder="t('inventory.select_products')"
            multiple
            filterable
            clearable
            :disabled="saving"
          />
          <p class="helper">{{ t("inventory.products_help") }}</p>
        </div>
        <div v-if="model.kind === 'static'">
          <label class="field-label" for="static-content">{{
            t("inventory.static_content")
          }}</label>
          <n-input
            id="static-content"
            v-model:value="model.static_content"
            type="textarea"
            :maxlength="65536"
            :autosize="{ minRows: 8, maxRows: 18 }"
            :placeholder="
              model.id
                ? t('inventory.static_keep')
                : t('inventory.static_placeholder')
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
      }}</n-button>
      <n-button
        attr-type="submit"
        form="inventory-form"
        type="primary"
        :loading="saving"
        >{{ t("inventory.save") }}</n-button
      >
    </template>
  </FormDialog>

  <FormDialog
    v-model:show="importing"
    :title="t('stock.import_title', { name: stockPool?.name ?? '' })"
    :busy="importBusy"
  >
    <p class="muted">{{ t("stock.import_help") }}</p>
    <n-input
      v-model:value="stockText"
      type="textarea"
      :autosize="{ minRows: 8, maxRows: 18 }"
      :placeholder="t('stock.placeholder')"
      :input-props="{ 'aria-label': t('stock.content') }"
    />
    <n-alert v-if="importError" type="error" class="space-top">{{
      errorText(importError)
    }}</n-alert>
    <template #footer>
      <n-button :disabled="importBusy" @click="importing = false">{{
        t("common.cancel")
      }}</n-button>
      <n-button type="primary" :loading="importBusy" @click="importItems">{{
        t("stock.import")
      }}</n-button>
    </template>
  </FormDialog>

  <FormDialog
    v-model:show="stockDialog"
    :title="t('stock.title', { name: stockPool?.name ?? '' })"
    :busy="!!stockDeleting"
  >
    <n-alert v-if="stockError" type="error"
      >{{ errorText(stockError) }}
      <n-button
        text
        :loading="stockLoading"
        @click="stockPool && viewStock(stockPool)"
        >{{ t("common.recheck") }}</n-button
      >
    </n-alert>
    <div :aria-busy="stockLoading">
      <div v-if="stockItems.length || stockLoading" class="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{{ t("stock.id") }}</th>
              <th>{{ t("common.status") }}</th>
              <th>{{ t("common.actions") }}</th>
            </tr>
          </thead>
          <tbody>
            <template v-if="stockLoading && !stockItems.length">
              <tr v-for="row in 3" :key="row" aria-hidden="true">
                <td v-for="column in 3" :key="column"><n-skeleton text /></td>
              </tr>
            </template>
            <tr v-for="item in stockItems" :key="item.id">
              <td class="code-cell">{{ item.id }}</td>
              <td>{{ stockNames[item.status] }}</td>
              <td>
                <n-button
                  v-if="item.status === 'available'"
                  size="small"
                  type="error"
                  secondary
                  :disabled="!!stockDeleting"
                  :loading="stockDeleting === item.id"
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
      />
    </div>
    <p class="helper">{{ t("stock.list_help") }}</p>
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
.name-cell small {
  display: block;
}
.table-scroll .row-actions {
  flex-wrap: nowrap;
}
.form-grid .field-label {
  margin-bottom: 7px;
}
</style>
