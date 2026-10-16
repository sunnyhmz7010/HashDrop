import { ref } from "vue";
import type { Order, OrderQuote } from "./api";

export const currentClaimKey = ref("");
const claimStorage = "hashdrop:claim";
const purchaseStorage = "hashdrop:checkout";

export interface SavedOrder {
  key: string;
  order: Order;
}
const historyPrefix = "hashdrop:history:";
export const historyStorageError = ref(false);
export const historyCleared = ref(0);
export const clearedOrderIDs = ref<string[]>([]);

function readOrderHistory(): SavedOrder[] {
  const records: SavedOrder[] = [];
  try {
    for (let index = 0; index < localStorage.length; index++) {
      const storageKey = localStorage.key(index);
      if (!storageKey?.startsWith(historyPrefix)) continue;
      try {
        const item = JSON.parse(localStorage.getItem(storageKey) ?? "null");
        if (
          item &&
          typeof item.key === "string" &&
          item.key.trim() &&
          Array.from(item.key).length <= 128 &&
          item.order &&
          typeof item.order.id === "string" &&
          /^[A-Za-z0-9_-]{22,64}$/.test(item.order.id) &&
          storageKey === historyPrefix + item.order.id &&
          typeof item.order.product_name === "string" &&
          typeof item.order.currency === "string" &&
          Number.isSafeInteger(item.order.total) &&
          item.order.total > 0 &&
          Number.isSafeInteger(item.order.created_at) &&
          Number.isSafeInteger(item.order.quantity) &&
          typeof item.order.payment_status === "string" &&
          typeof item.order.delivery_status === "string"
        )
          records.push(item);
        else historyStorageError.value = true;
      } catch {
        historyStorageError.value = true;
        console.warn("有本地订单历史无法读取，请凭已保存的密钥查询");
      }
    }
  } catch {
    historyStorageError.value = true;
    console.warn("浏览器不允许读取本地订单历史");
  }
  return records.sort(
    (a, b) =>
      b.order.created_at - a.order.created_at ||
      b.order.id.localeCompare(a.order.id),
  );
}
export const orderHistory = ref<SavedOrder[]>(readOrderHistory());

export function rememberOrder(order: Order, key: string): boolean {
  const record = { key, order: { ...order } };
  // 每笔订单独立写入，避免不同标签页下单时互相覆盖整份历史。
  orderHistory.value = [
    record,
    ...orderHistory.value.filter((item) => item.order.id !== order.id),
  ].sort(
    (a, b) =>
      b.order.created_at - a.order.created_at ||
      b.order.id.localeCompare(a.order.id),
  );
  let sessionSaved = false;
  try {
    sessionStorage.setItem(`hashdrop:${order.id}`, key);
    sessionSaved = true;
  } catch {
    console.warn("无法在标签页中保存订单领取凭证");
  }
  try {
    // 仅保存订单信息与领取凭证，不保存交付内容；查看交付仍须由服务器核验。
    localStorage.setItem(historyPrefix + order.id, JSON.stringify(record));
    historyStorageError.value = false;
    return true;
  } catch {
    historyStorageError.value = true;
    return sessionSaved;
  }
}

export function restoreOrderKey(id: string): string {
  return orderHistory.value.find((item) => item.order.id === id)?.key ?? "";
}

export function clearOrderHistory(): boolean {
  let success = true;
  const ids = orderHistory.value.map((item) => item.order.id);
  try {
    const keys: string[] = [];
    for (let index = 0; index < localStorage.length; index++) {
      const key = localStorage.key(index);
      if (key?.startsWith(historyPrefix)) keys.push(key);
    }
    for (const key of keys) localStorage.removeItem(key);
    orderHistory.value = [];
  } catch {
    success = false;
    console.warn("无法清除部分本地订单历史");
  }
  currentClaimKey.value = "";
  try {
    sessionStorage.removeItem(claimStorage);
    for (const id of ids) sessionStorage.removeItem(`hashdrop:${id}`);
    const pending =
      pendingPurchase.value ??
      JSON.parse(sessionStorage.getItem(purchaseStorage) ?? "null");
    if (pending && ids.includes(pending.requestID)) {
      sessionStorage.removeItem(purchaseStorage);
      pendingPurchase.value = undefined;
    }
  } catch {
    success = false;
    console.warn("无法清除部分浏览器领取凭证");
  }
  historyStorageError.value = !success;
  clearedOrderIDs.value = ids;
  historyCleared.value++;
  return success;
}

window.addEventListener("storage", (event) => {
  if (event.key === null || event.key.startsWith(historyPrefix)) {
    const removedIDs = event.key
      ? [event.key.slice(historyPrefix.length)]
      : orderHistory.value.map((item) => item.order.id);
    orderHistory.value = readOrderHistory();
    if (event.newValue === null) {
      forgetClaimKey();
      for (const id of removedIDs) forgetPurchase(id);
      if (event.key) {
        try {
          sessionStorage.removeItem(
            `hashdrop:${event.key.slice(historyPrefix.length)}`,
          );
        } catch {
          console.warn("无法清除标签页中对应的领取凭证");
        }
      }
      clearedOrderIDs.value = removedIDs;
      historyCleared.value++;
    }
  }
});

export interface PurchaseIntent {
  requestID: string;
  productID: string;
  quantity: number;
  key: string;
  couponCode?: string;
  quote?: OrderQuote;
}
const pendingPurchase = ref<PurchaseIntent>();

function randomText(length: number): string {
  const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789";
  let value = "";
  while (value.length < length) {
    const bytes = crypto.getRandomValues(new Uint8Array(length));
    for (const byte of bytes) {
      // 拒绝无法均分到 36 个字符的尾部值，避免取模偏差。
      if (byte < 252 && value.length < length) value += alphabet[byte % 36];
    }
  }
  return value;
}

export function generateClaimKey(): string {
  let key: string;
  do {
    key = randomText(24);
  } while (!/[a-z]/.test(key) || !/[0-9]/.test(key));
  return key;
}

export function newPurchaseID(): string {
  return randomText(32);
}

export function rememberClaimKey(key: string): boolean {
  currentClaimKey.value = key;
  try {
    sessionStorage.setItem(claimStorage, key);
    return true;
  } catch {
    return false;
  }
}

export function restoreClaimKey(): string {
  if (currentClaimKey.value) return currentClaimKey.value;
  try {
    return (
      sessionStorage.getItem(claimStorage) || orderHistory.value[0]?.key || ""
    );
  } catch {
    console.warn("无法读取浏览器中的领取密钥");
    return orderHistory.value[0]?.key ?? "";
  }
}

export function forgetClaimKey(): void {
  currentClaimKey.value = "";
  try {
    sessionStorage.removeItem(claimStorage);
  } catch {
    console.warn("无法清理浏览器中的领取密钥");
  }
}

export function restorePurchase(productID: string): PurchaseIntent | undefined {
  if (pendingPurchase.value?.productID === productID)
    return pendingPurchase.value;
  try {
    const value = JSON.parse(sessionStorage.getItem(purchaseStorage) ?? "null");
    if (
      value?.productID === productID &&
      typeof value.requestID === "string" &&
      typeof value.key === "string" &&
      Number.isInteger(value.quantity) &&
      value.quantity >= 1 &&
      value.quantity <= 100
    )
      return value;
  } catch {
    console.warn("无法读取待确认订单，可凭已保存的密钥查询");
  }
  return undefined;
}

export function rememberPurchase(intent: PurchaseIntent): boolean {
  pendingPurchase.value = intent;
  try {
    sessionStorage.setItem(purchaseStorage, JSON.stringify(intent));
    return true;
  } catch {
    return false;
  }
}

export function forgetPurchase(requestID: string): void {
  if (pendingPurchase.value?.requestID === requestID)
    pendingPurchase.value = undefined;
  try {
    const intent = JSON.parse(
      sessionStorage.getItem(purchaseStorage) ?? "null",
    );
    if (intent?.requestID === requestID)
      sessionStorage.removeItem(purchaseStorage);
  } catch {
    console.warn("无法清理浏览器中的待确认订单记录");
  }
}
