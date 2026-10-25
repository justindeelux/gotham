import { useMessage } from "naive-ui";
import { computed, getCurrentScope, onScopeDispose, ref } from "vue";

import {
  describeFilesError,
  describeUploadError,
  isCanceledError,
  uploadFiles,
} from "@/features/files/api/files";
import type { UploadFilesOptions } from "@/features/files/api/files";
import { validateEntryName } from "@/features/files/utils/files";
import { i18n } from "@/shared/i18n";

export type UploadStatus =
  | "queued"
  | "uploading"
  | "done"
  | "conflict"
  | "error"
  | "cancelled";

export interface UploadItem {
  id: string;
  file: File;
  status: UploadStatus;
  loaded: number;
  total: number;
  errorToken: string;
  message: string;
  controller: AbortController | null;
}

export interface UploadTarget {
  serverId: string;
  root: string;
  path: string;
}

let uploadSeq = 0;

interface UploadWork {
  id: string;
  overwrite: boolean;
}

export function useFileUpload() {
  const message = useMessage();
  const items = ref<UploadItem[]>([]);
  const uploading = ref(false);

  const queue: UploadWork[] = [];
  let draining = false;
  let cancelled = false;
  let activeTarget: UploadTarget | null = null;

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

  const pending = computed<number>(
    () => summary.value.queued + summary.value.uploading,
  );

  const hasFailures = computed<number>(
    () => summary.value.error + summary.value.conflict,
  );

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

  function removeItem(id: string): void {
    const item = items.value.find((candidate) => candidate.id === id);
    if (!item || item.status === "uploading") {
      return;
    }
    items.value = items.value.filter((candidate) => candidate.id !== id);
  }

  function clear(): void {
    cancelAll();
    items.value = [];
  }

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
    const name = item.file.name.trim();
    const nameError = validateEntryName(name);
    if (nameError) {
      item.status = "error";
      item.errorToken = "invalid";
      item.message = String(i18n.global.t(nameError));
      item.controller = null;
      return;
    }
    if (name !== item.file.name) {
      item.file = new File([item.file], name, {
        type: item.file.type,
        lastModified: item.file.lastModified,
      });
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

  async function run(): Promise<void> {
    if (draining) {
      return;
    }
    draining = true;
    uploading.value = true;
    try {
      while (queue.length > 0) {
        if (cancelled) {
          queue.length = 0;
          break;
        }
        const work = queue.shift();
        if (!work || !activeTarget) {
          continue;
        }
        const item = items.value.find((candidate) => candidate.id === work.id);
        if (
          !item ||
          (item.status !== "queued" &&
            item.status !== "error" &&
            item.status !== "conflict")
        ) {
          continue;
        }
        await uploadOne(item, activeTarget, work.overwrite);
      }
    } finally {
      draining = false;
      uploading.value = false;
    }
    if (summary.value.error > 0) {
      message.error(describeUploadError("error"));
    }
  }

  function enqueue(work: UploadWork[]): Promise<void> {
    if (work.length > 0) {
      queue.push(...work);
      for (const entry of work) {
        const item = items.value.find((candidate) => candidate.id === entry.id);
        if (item) {
          item.status = "queued";
          item.errorToken = "";
          item.message = "";
        }
      }
    }
    return run();
  }

  function start(target: UploadTarget): Promise<void> {
    if (uploading.value || items.value.every((item) => item.status !== "queued")) {
      return Promise.resolve();
    }
    cancelled = false;
    activeTarget = target;
    const work = items.value
      .filter((item) => item.status === "queued")
      .map((item) => ({ id: item.id, overwrite: false }));
    return enqueue(work);
  }

  function cancel(id: string): void {
    const item = items.value.find((candidate) => candidate.id === id);
    if (item?.status === "uploading") {
      item.controller?.abort();
    }
  }

  function cancelAll(): void {
    cancelled = true;
    queue.length = 0;
    for (const item of items.value) {
      if (item.status === "uploading") {
        item.controller?.abort();
      }
    }
  }

  function retry(
    id: string,
    target: UploadTarget,
    overwrite = false,
  ): Promise<void> {
    const item = items.value.find((candidate) => candidate.id === id);
    if (!item || (item.status !== "error" && item.status !== "conflict")) {
      return Promise.resolve();
    }
    cancelled = false;
    activeTarget = target;
    return enqueue([{ id, overwrite }]);
  }

  function retryFailed(target: UploadTarget): Promise<void> {
    cancelled = false;
    activeTarget = target;
    const work = items.value
      .filter((item) => item.status === "error" || item.status === "conflict")
      .map((item) => ({ id: item.id, overwrite: item.status === "conflict" }));
    return enqueue(work);
  }

  if (getCurrentScope()) {
    onScopeDispose(() => {
      cancelAll();
    });
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
