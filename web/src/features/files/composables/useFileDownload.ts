import { useMessage } from "naive-ui";
import { ref } from "vue";

import { describeFilesError, downloadUrl, issueDownloadTicket } from "@/features/files/api/files";
import type { FilesEntry } from "@/features/files/schemas/files";
import { joinPath } from "@/features/files/utils/files";

/**
 * Click-through download for the file browser. Mints a single-use ticket
 * through the authenticated API, then hands the ticket URL to the browser as a
 * native download (the download route cannot attach the bearer token, so the
 * ticket is its credential). Any failure surfaces a curated, localized
 * message; the raw server text is never shown.
 */
export function useFileDownload() {
  const message = useMessage();
  const pending = ref(false);

  /**
   * trigger hands a same-origin attachment URL to the browser. The `download`
   * attribute keeps a failed download (a JSON error body) from navigating the
   * SPA away from its route: the browser saves the response instead of
   * replacing the document. On success the server's Content-Disposition still
   * supplies the real filename.
   */
  function trigger(href: string, filename: string): void {
    if (typeof document === "undefined") {
      return;
    }
    const link = document.createElement("a");
    link.href = href;
    link.download = filename;
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
      trigger(downloadUrl(serverId, ticket), entry.name);
      return true;
    } catch (err) {
      message.error(describeFilesError(err));
      return false;
    } finally {
      pending.value = false;
    }
  }

  return { pending, download };
}
