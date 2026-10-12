import { useMessage } from "naive-ui";
import { computed, ref } from "vue";

import {
  describeFilesError,
  describeUploadError,
  isCanceledError,
  uploadFiles,
} from "@/features/files/api/files";
import type { UploadFilesOptions } from "@/features/files/api/files";
import { validateEntryName } from "@/features/files/utils/files";
import { i18n } from "@/shared/i18n";

/**
 * Upload queue behind the step-7 upload dialog. Each file is one small
 * multipart request against POST /files/upload, so progress, cancel and retry
 * are genuinely per file and one failure never aborts the rest. Files are sent
 * one at a time to stay under the node's concurrent-transfer budget, and the
 * request timeout is disabled (the server bounds the body) so a large file is
 * not cut by the 15 s default.
 *
 * Every item ends in a terminal state with a localized message, so a partial
 * batch reports a per-file outcome plus a footer summary.
 */
export type UploadStatus =
  | "queued"
  | "uploading"
  | "done"
  | "conflict"
  | "error"
  | "cancelled";

/** One queued upload and its live progress. */
export interface UploadItem {
  id: string;
  file: File;
  status: UploadStatus;
  /** loaded bytes of the current request (a proxy for body progress). */
  loaded: number;
  /** total body bytes (falls back to the file size). */
  total: number;
  /** server error token ("conflict", "too_large"…) for a failed item. */
  errorToken: string;
  /** localized failure text, empty while queued/uploading/done. */
  message: string;
  /** controller of the in-flight request, so cancel can abort it. */
  controller: AbortController | null;
}

/** UploadTarget is the directory a batch uploads into. */
export interface UploadTarget {
  serverId: string;
  teamId: string;
  root: string;
  path: string;
}

let uploadSeq = 0;

export function useFileUpload() {
  const message = useMessage();
  const items = ref<UploadItem[]>([]);
  const uploading = ref(false);

  const summary = computed(() => {
    const counts: Record<UploadStatus, number> = {
      queued: 0,
      uploading: 0,
      done: 0,
      conflict: 0,
      error: 0,
      cancelled: 0,
    };
    for (const item of items.value) {
      counts[item.status] += 1;
    }
    return counts;
  });

  /** pending reports files still to send (queued or uploading). */
  const pending = computed<number>(
    () => summary.value.queued + summary.value.uploading,
  );

  /** hasFailures reports error/conflict rows the retry action can re-send. */
  const hasFailures = computed<number>(
    () => summary.value.error + summary.value.conflict,
  );

  /** addFiles queues dropped or picked files (deduplicating is not needed). */
  function addFiles(files: File[] | FileList): void {
    for (const file of Array.from(files)) {
      uploadSeq += 1;
      items.value.push({
        id: `upload-${uploadSeq}`,
        file,
        status: "queued",
        loaded: 0,
        total: file.size,
        errorToken: "",
        message: "",
        controller: null,
      });
    }
  }

  /** removeItem drops one row that has not been sent yet. */
  function removeItem(id: string): void {
    const item = items.value.find((candidate) => candidate.id === id);
    if (!item || item.status === "uploading") {
      return;
    }
    items.value = items.value.filter((candidate) => candidate.id !== id);
  }

  /** clear resets the whole queue. */
  function clear(): void {
    for (const item of items.value) {
      item.controller?.abort();
    }
    items.value = [];
    uploading.value = false;
  }

  /** uploadOne sends one file and records its terminal state. */
  async function uploadOne(
    item: UploadItem,
    target: UploadTarget,
    overwrite: boolean,
  ): Promise<void> {
    const controller = new AbortController();
    item.controller = controller;
    item.status = "uploading";
    item.errorToken = "";
    item.message = "";
    item.loaded = 0;
    // A local name check fails fast and shows the same rule as the create
    // dialogs, instead of a round trip the server would reject anyway.
    const nameError = validateEntryName(item.file.name);
    if (nameError) {
      item.status = "error";
      item.errorToken = "invalid";
      item.message = String(i18n.global.t(nameError));
      item.controller = null;
      return;
    }
    try {
      const options: UploadFilesOptions = {
        root: target.root,
        path: target.path,
        overwrite,
        signal: controller.signal,
        onProgress: (progress) => {
          item.loaded = progress.loaded;
          item.total = progress.total > 0 ? progress.total : item.file.size;
        },
      };
      const results = await uploadFiles(
        target.serverId,
        target.teamId,
        [item.file],
        options,
      );
      const result = results[0];
      if (!result) {
        item.status = "error";
        item.errorToken = "error";
        item.message = describeUploadError("error");
        return;
      }
      if (result.error) {
        item.errorToken = result.error;
        item.status = result.error === "conflict" ? "conflict" : "error";
        item.message = describeUploadError(result.error);
        return;
      }
      item.status = "done";
      item.loaded = item.total;
    } catch (err) {
      if (controller.signal.aborted || isCanceledError(err)) {
        // A cancel is a deliberate user action, not a failure: no toast.
        item.status = "cancelled";
        item.message = "";
        return;
      }
      item.status = "error";
      item.errorToken = "error";
      item.message = describeFilesError(err);
    } finally {
      item.controller = null;
    }
  }

  /**
   * start sends every queued file, one at a time, and toasts a one-line
   * summary when failures remain. The caller refreshes the listing on success
   * (the dialog shows the per-file rows meanwhile).
   */
  async function start(target: UploadTarget): Promise<void> {
    if (uploading.value) {
      return;
    }
    uploading.value = true;
    try {
      for (const item of items.value) {
        if (item.status !== "queued") {
          continue;
        }
        await uploadOne(item, target, false);
      }
    } finally {
      uploading.value = false;
    }
    if (summary.value.error > 0) {
      message.error(describeUploadError("error"));
    }
  }

  /** cancel aborts the in-flight request of one row. */
  function cancel(id: string): void {
    const item = items.value.find((candidate) => candidate.id === id);
    if (item?.status === "uploading") {
      item.controller?.abort();
    }
  }

  /** cancelAll aborts the in-flight request (queued rows stay queued). */
  function cancelAll(): void {
    for (const item of items.value) {
      if (item.status === "uploading") {
        item.controller?.abort();
      }
    }
  }

  /** retry re-sends one row; overwrite replaces a conflicting name. */
  async function retry(
    id: string,
    target: UploadTarget,
    overwrite = false,
  ): Promise<void> {
    const item = items.value.find((candidate) => candidate.id === id);
    if (!item || item.status === "uploading") {
      return;
    }
    uploading.value = true;
    try {
      await uploadOne(item, target, overwrite);
    } finally {
      uploading.value = false;
    }
  }

  /**
   * retryFailed re-sends every failed row: a plain failure retries as-is, a
   * conflict retries with overwrite (the user retrying a skipped file means
   * replace).
   */
  async function retryFailed(target: UploadTarget): Promise<void> {
    if (uploading.value) {
      return;
    }
    uploading.value = true;
    try {
      for (const item of items.value) {
        if (item.status === "error") {
          await uploadOne(item, target, false);
        } else if (item.status === "conflict") {
          await uploadOne(item, target, true);
        }
      }
    } finally {
      uploading.value = false;
    }
  }

  return {
    items,
    uploading,
    summary,
    pending,
    hasFailures,
    addFiles,
    removeItem,
    clear,
    start,
    cancel,
    cancelAll,
    retry,
    retryFailed,
  };
}
