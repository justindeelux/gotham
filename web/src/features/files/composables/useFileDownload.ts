import { useMessage } from "naive-ui";
import { ref } from "vue";

import { describeFilesError, downloadUrl, issueDownloadTicket } from "@/features/files/api/files";
import { joinPath } from "@/features/files/utils/files";

export function useFileDownload() {
  const message = useMessage();
  const pending = ref(false);

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

  async function download(
    serverId: string,
    root: string,
    directory: string,
    entry: { name: string },
  ): Promise<boolean> {
    if (pending.value) {
      return false;
    }
    pending.value = true;
    try {
      const ticket = await issueDownloadTicket(
        serverId,
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
