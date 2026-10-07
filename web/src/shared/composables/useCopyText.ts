import { useClipboard } from "@vueuse/core";
import { useMessage } from "naive-ui";

import { i18n } from "@/shared/i18n";

/**
 * useCopyText copies a string to the clipboard and toasts the result.
 * useClipboard runs with legacy:true so plain-http pages (no async
 * Clipboard API, like the shared test box) still copy through the
 * textarea+execCommand fallback instead of resolving as a silent no-op.
 * The success toast fires only after copy() resolves with copied set; any
 * failure (unsupported target, rejected write, failed fallback) shows the
 * error toast. Toasts resolve from the shared catalog in the current locale
 * at invocation time; the value, label, API shape, legacy fallback and
 * success/error outcomes are unchanged.
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
      message.success(
        String(i18n.global.t("common.clipboard.copied", { label })),
      );
    } catch {
      message.error(
        String(
          i18n.global.t("common.clipboard.copyFailed", {
            label: label.toLowerCase(),
          }),
        ),
      );
    }
  }

  return { copyText };
}
