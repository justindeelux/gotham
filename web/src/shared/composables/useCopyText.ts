import { useClipboard } from "@vueuse/core";
import { useMessage } from "naive-ui";

/**
 * useCopyText copies a string to the clipboard and toasts the result.
 * useClipboard runs with legacy:true so plain-http pages (no async
 * Clipboard API, like the shared test box) still copy through the
 * textarea+execCommand fallback instead of resolving as a silent no-op.
 * The success toast fires only after copy() resolves with copied set; any
 * failure (unsupported target, rejected write, failed fallback) shows the
 * error toast. Both strings match the pre-vueuse helpers byte for byte.
 */
export function useCopyText() {
  const message = useMessage();
  const { copy, copied } = useClipboard({ legacy: true });

  /** copyText copies value and confirms with a toast, or errors. */
  async function copyText(value: string, label: string): Promise<void> {
    try {
      await copy(value);
      if (!copied.value) {
        throw new Error("Copy was not confirmed");
      }
      message.success(`${label} copied to clipboard`);
    } catch {
      message.error(`Could not copy ${label.toLowerCase()}`);
    }
  }

  return { copyText };
}
