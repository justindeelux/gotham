import type { Locale } from "./locale";

export interface StaleChunkCopy {
  message: string;
  reload: string;
}

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
