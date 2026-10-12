import { useMessage } from "naive-ui";
import { ref } from "vue";
import { useI18n } from "vue-i18n";

import { describeFilesError, downloadUrl, issueDownloadTicket } from "@/features/files/api/files";
import type { FilesEntry } from "@/features/files/schemas/files";
import { joinPath } from "@/features/files/utils/files";

/**
 * Click-through download for the read-only browser. Mints a single-use ticket
 * through the authenticated API, then hands the ticket URL to the browser as a
 * native download (the download route cannot attach the bearer token, so the
 * ticket is its credential). Any failure surfaces a curated, localized
 * message; the raw server text is never shown.
 */
export function useFileDownload() {
  const { t } = useI18n();
  const message = useMessage();
  const pending = ref(false);

  /** trigger hands a same-origin attachment URL to the browser. */
  function trigger(href: string): void {
    if (typeof document === "undefined") {
      return;
    }
    const link = document.createElement("a");
    link.href = href;
    link.rel = "noopener";
    document.body.appendChild(link);
    link.click();
    link.remove();
  }

  /**
   * download mints a ticket for one entry and starts the native download.
   * Returns true when the download was triggered, false on any failure.
   */
  async function download(
    serverId: string,
    teamId: string,
    root: string,
    directory: string,
    entry: FilesEntry,
  ): Promise<boolean> {
    if (pending.value) {
      return false;
    }
    pending.value = true;
    try {
      const ticket = await issueDownloadTicket(
        serverId,
        teamId,
        root,
        joinPath(directory, entry.name),
      );
      if (ticket === "") {
        throw new Error("empty download ticket");
      }
      trigger(downloadUrl(serverId, ticket));
      return true;
    } catch (err) {
      message.error(describeFilesError(err) || t("files.download.failed"));
      return false;
    } finally {
      pending.value = false;
    }
  }

  return { pending, download };
}
