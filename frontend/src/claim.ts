import { ref } from "vue";
import type { OrderQuote } from "./api";

export const currentClaimKey = ref("");
const claimStorage = "hashdrop:claim";
const purchaseStorage = "hashdrop:checkout";

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
    return sessionStorage.getItem(claimStorage) ?? "";
  } catch {
    console.warn("无法读取浏览器中的领取密钥");
    return "";
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
