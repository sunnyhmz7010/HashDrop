import type { ContentBlock, ContentDocument } from "./api";

export function blockID(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(12));
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join(
    "",
  );
}
export function textContent(text = ""): ContentDocument {
  return { blocks: [{ id: blockID(), type: "text", text }] };
}
export function stockPlan(label: string): ContentDocument {
  return { blocks: [{ id: blockID(), type: "stock", label, quantity: 1 }] };
}
export function cloneContent(content: ContentDocument): ContentDocument {
  return { blocks: content.blocks.map((block) => ({ ...block })) };
}
export function safeContentLink(value?: string): string | undefined {
  try {
    const url = new URL(value ?? "");
    return ["https:", "http:"].includes(url.protocol) &&
      !url.username &&
      !url.password
      ? url.href
      : undefined;
  } catch {
    return undefined;
  }
}
export function hasContent(document: ContentDocument): boolean {
  return (
    document.blocks.length > 0 &&
    document.blocks.every((block: ContentBlock) => {
      if (block.type === "text") return !!block.text?.trim();
      if (block.type === "link") return !!safeContentLink(block.url);
      if (block.type === "stock")
        return (
          Number.isInteger(block.quantity ?? 1) &&
          (block.quantity ?? 1) >= 0 &&
          (block.quantity ?? 1) <= 100
        );
      return false;
    })
  );
}
