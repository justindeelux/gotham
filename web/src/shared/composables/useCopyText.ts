import { useClipboard } from "@vueuse/core";
import { useMessage } from "naive-ui";

import { i18n } from "@/shared/i18n";

export function useCopyText() {
  const message = useMessage();
  const { copy, copied } = useClipboard({ legacy: true });

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
