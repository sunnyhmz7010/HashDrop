import { ref } from "vue";
import type { Donation } from "./api";

interface SavedDonation {
  donation: Donation;
  token: string;
}
const prefix = "hashdrop:donation-history:";
export const donationStorageError = ref(false);
export const donationsCleared = ref<string[]>([]);
function read(): SavedDonation[] {
  const records: SavedDonation[] = [];
  try {
    for (let index = 0; index < localStorage.length; index++) {
      const key = localStorage.key(index);
      if (!key?.startsWith(prefix)) continue;
      try {
        const record = JSON.parse(localStorage.getItem(key) ?? "null");
        if (
          record &&
          /^don_[A-Za-z0-9_-]{22,64}$/.test(record.donation?.id) &&
          key === prefix + record.donation.id &&
          typeof record.token === "string" &&
          /^[A-Za-z0-9_-]{43}$/.test(record.token) &&
          typeof record.donation.title === "string" &&
          typeof record.donation.currency === "string" &&
          typeof record.donation.payment_status === "string" &&
          Number.isSafeInteger(record.donation.amount) &&
          record.donation.amount > 0 &&
          Number.isSafeInteger(record.donation.created_at)
        )
          records.push(record);
        else donationStorageError.value = true;
      } catch {
        donationStorageError.value = true;
      }
    }
  } catch {
    donationStorageError.value = true;
  }
  return records.sort(
    (a, b) =>
      b.donation.created_at - a.donation.created_at ||
      b.donation.id.localeCompare(a.donation.id),
  );
}
export const donationHistory = ref<SavedDonation[]>(read());
export function donationToken(id: string): string {
  return (
    donationHistory.value.find((record) => record.donation.id === id)?.token ??
    ""
  );
}
export function rememberDonation(donation: Donation, token: string): boolean {
  const record = { donation: { ...donation }, token };
  donationHistory.value = [
    record,
    ...donationHistory.value.filter((item) => item.donation.id !== donation.id),
  ].sort(
    (a, b) =>
      b.donation.created_at - a.donation.created_at ||
      b.donation.id.localeCompare(a.donation.id),
  );
  let saved = false;
  try {
    sessionStorage.setItem(`hashdrop:donation:${donation.id}`, token);
    saved = true;
  } catch {
    console.warn("无法在标签页中保存捐赠凭证");
  }
  try {
    localStorage.setItem(prefix + donation.id, JSON.stringify(record));
    donationStorageError.value = false;
    return true;
  } catch {
    donationStorageError.value = true;
    return saved;
  }
}
function clearSession(ids: string[]): boolean {
  try {
    for (const id of ids) sessionStorage.removeItem(`hashdrop:donation:${id}`);
    return true;
  } catch {
    console.warn("无法清除部分捐赠凭证");
    return false;
  }
}
export function clearDonationHistory(): boolean {
  const ids = donationHistory.value.map((record) => record.donation.id);
  let success = true;
  try {
    const keys = Object.keys(localStorage).filter((key) =>
      key.startsWith(prefix),
    );
    for (const key of keys) localStorage.removeItem(key);
    donationHistory.value = [];
  } catch {
    success = false;
  }
  success = clearSession(ids) && success;
  donationStorageError.value = !success;
  donationsCleared.value = ids;
  return success;
}
window.addEventListener("storage", (event) => {
  if (
    event.storageArea !== localStorage ||
    (event.key !== null && !event.key.startsWith(prefix))
  )
    return;
  const ids = event.key
    ? [event.key.slice(prefix.length)]
    : donationHistory.value.map((record) => record.donation.id);
  donationHistory.value = read();
  if (event.newValue === null) {
    if (!clearSession(ids)) donationStorageError.value = true;
    donationsCleared.value = ids;
  }
});
