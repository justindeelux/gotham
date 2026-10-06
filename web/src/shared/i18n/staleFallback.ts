/**
 * Out-of-Vue stale-chunk fallback copy. main.ts renders this when the
 * initial navigation fails after the one-shot reload, outside the mounted
 * app so no feature chunk (and no vue-i18n runtime) is required.
 */
import type { Locale } from "./locale";

export interface StaleChunkCopy {
  message: string;
  reload: string;
}

/** staleChunkCopy returns the fallback text for a locale (English default). */
export function staleChunkCopy(locale: Locale): StaleChunkCopy {
  if (locale === "vi") {
    return {
      message:
        "Ứng dụng không tải xong. Vui lòng tải lại trang để nhận bản mới nhất.",
      reload: "Tải lại",
    };
  }
  return {
    message:
      "The application could not finish loading. Please reload the page.",
    reload: "Reload",
  };
}
