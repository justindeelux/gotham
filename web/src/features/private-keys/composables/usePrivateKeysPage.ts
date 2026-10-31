import { useMessage } from "naive-ui";
import { computed, onMounted, ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  createPrivateKey,
  deletePrivateKey,
  describePrivateKeyError,
  generatePrivateKey,
  listPrivateKeys,
  listServersUsingKeys,
  revealPrivateKey,
  updatePrivateKey,
} from "@/features/private-keys/api/keys";
import {
  editPrivateKeyFormSchema,
  generatePrivateKeyFormSchema,
  importPrivateKeyFormSchema,
} from "@/features/private-keys/schemas/keys";
import type {
  PrivateKey,
  PrivateKeyType,
} from "@/features/private-keys/schemas/keys";
import { randomKeyName } from "@/features/private-keys/utils/randomName";
import type { Server } from "@/features/servers";

export type CreateKeyTab = "generate" | "import";

export function usePrivateKeysPage() {
  const { t } = useI18n();
  const message = useMessage();

  const keys = ref<PrivateKey[]>([]);
  const servers = ref<Server[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const failure = ref<unknown>(null);
  const errorMessage = computed<string>(() =>
    failure.value === null
      ? ""
      : describePrivateKeyError(
          failure.value,
          t("private-keys.errors.loadFailed"),
        ),
  );

  const usageById = computed<Record<string, number>>(() => {
    const counts: Record<string, number> = {};
    for (const server of servers.value) {
      if (server.ssh_key_id) {
        counts[server.ssh_key_id] = (counts[server.ssh_key_id] ?? 0) + 1;
      }
    }
    return counts;
  });

  const createOpen = ref(false);
  const createTab = ref<CreateKeyTab>("generate");
  const createName = ref("");
  const createDescription = ref("");
  const createType = ref<PrivateKeyType>("ed25519");
  const importPrivateKeyValue = ref("");
  const importPublicKeyValue = ref("");
  const createTouched = ref(false);
  const createBusy = ref(false);
  const createFailure = ref<unknown>(null);
  const createErrorMessage = computed<string>(() =>
    createFailure.value === null
      ? ""
      : describePrivateKeyError(
          createFailure.value,
          t("private-keys.errors.createFailed"),
        ),
  );

  const typeOptions = computed(() => [
    { label: "Ed25519", value: "ed25519" },
    { label: "RSA", value: "rsa" },
    { label: "ECDSA", value: "ecdsa" },
  ]);

  function createIssues() {
    return (createTab.value === "generate"
      ? generatePrivateKeyFormSchema.safeParse({
          name: createName.value,
          description: createDescription.value,
          type: createType.value,
        })
      : importPrivateKeyFormSchema.safeParse({
          name: createName.value,
          description: createDescription.value,
          private_key: importPrivateKeyValue.value,
          public_key: importPublicKeyValue.value,
        })
    );
  }

  const createNameError = computed<string | null>(() => {
    if (!createTouched.value) {
      return null;
    }
    const result = createIssues();
    if (result.success) {
      return null;
    }
    const issue = result.error.issues.find((item) => item.path[0] === "name");
    return issue ? String(t(issue.message)) : null;
  });

  const importPrivateKeyError = computed<string | null>(() => {
    if (!createTouched.value || createTab.value !== "import") {
      return null;
    }
    const result = createIssues();
    if (result.success) {
      return null;
    }
    const issue = result.error.issues.find(
      (item) => item.path[0] === "private_key",
    );
    return issue ? String(t(issue.message)) : null;
  });

  const editing = ref<PrivateKey | null>(null);
  const editName = ref("");
  const editDescription = ref("");
  const editTouched = ref(false);
  const editBusy = ref(false);
  const editFailure = ref<unknown>(null);
  const editErrorMessage = computed<string>(() =>
    editFailure.value === null
      ? ""
      : describePrivateKeyError(
          editFailure.value,
          t("private-keys.errors.updateFailed"),
        ),
  );
  const editNameError = computed<string | null>(() => {
    if (!editTouched.value) {
      return null;
    }
    const result = editPrivateKeyFormSchema.safeParse({
      name: editName.value,
      description: editDescription.value,
    });
    if (result.success) {
      return null;
    }
    const issue = result.error.issues.find((item) => item.path[0] === "name");
    return issue ? String(t(issue.message)) : null;
  });

  const viewing = ref<PrivateKey | null>(null);
  const showing = ref<{ key: PrivateKey; privateKey: string } | null>(null);
  const revealingId = ref<string | null>(null);
  const removingId = ref<string | null>(null);

  async function refresh(): Promise<void> {
    loading.value = true;
    failure.value = null;
    try {
      const [loadedKeys, loadedServers] = await Promise.all([
        listPrivateKeys(),
        listServersUsingKeys(),
      ]);
      keys.value = loadedKeys;
      servers.value = loadedServers;
      loaded.value = true;
    } catch (error) {
      failure.value = error;
    } finally {
      loading.value = false;
    }
  }

  function openCreate(): void {
    createTab.value = "generate";
    createName.value = randomKeyName();
    createDescription.value = "";
    createType.value = "ed25519";
    importPrivateKeyValue.value = "";
    importPublicKeyValue.value = "";
    createTouched.value = false;
    createFailure.value = null;
    createOpen.value = true;
  }

  function closeCreate(): void {
    createOpen.value = false;
    createTouched.value = false;
    createFailure.value = null;
    importPrivateKeyValue.value = "";
    importPublicKeyValue.value = "";
  }

  function touchCreate(): void {
    createTouched.value = true;
  }

  async function submitCreate(): Promise<void> {
    createTouched.value = true;
    if (createBusy.value) {
      return;
    }
    createBusy.value = true;
    createFailure.value = null;
    try {
      if (createTab.value === "generate") {
        const parsed = generatePrivateKeyFormSchema.safeParse({
          name: createName.value,
          description: createDescription.value,
          type: createType.value,
        });
        if (!parsed.success) {
          return;
        }
        await generatePrivateKey(parsed.data);
      } else {
        const parsed = importPrivateKeyFormSchema.safeParse({
          name: createName.value,
          description: createDescription.value,
          private_key: importPrivateKeyValue.value,
          public_key: importPublicKeyValue.value,
        });
        if (!parsed.success) {
          return;
        }
        await createPrivateKey(parsed.data);
      }
      closeCreate();
      message.success(t("private-keys.toast.created"));
      await refresh();
    } catch (error) {
      createFailure.value = error;
    } finally {
      createBusy.value = false;
    }
  }

  function openView(key: PrivateKey): void {
    viewing.value = key;
  }

  function closeView(): void {
    viewing.value = null;
  }

  function openEdit(key: PrivateKey): void {
    editing.value = key;
    editName.value = key.name;
    editDescription.value = key.description ?? "";
    editTouched.value = false;
    editFailure.value = null;
  }

  function closeEdit(): void {
    editing.value = null;
    editTouched.value = false;
    editFailure.value = null;
  }

  function touchEdit(): void {
    editTouched.value = true;
  }

  async function submitEdit(): Promise<void> {
    editTouched.value = true;
    const key = editing.value;
    if (!key || editBusy.value) {
      return;
    }
    const parsed = editPrivateKeyFormSchema.safeParse({
      name: editName.value,
      description: editDescription.value,
    });
    if (!parsed.success) {
      return;
    }
    editBusy.value = true;
    editFailure.value = null;
    try {
      await updatePrivateKey(key.id, parsed.data);
      closeEdit();
      message.success(t("private-keys.toast.updated"));
      await refresh();
    } catch (error) {
      editFailure.value = error;
    } finally {
      editBusy.value = false;
    }
  }

  async function reveal(key: PrivateKey): Promise<void> {
    if (revealingId.value !== null) {
      return;
    }
    revealingId.value = key.id;
    try {
      const privateKey = await revealPrivateKey(key.id);
      showing.value = { key, privateKey };
    } catch (error) {
      message.error(
        describePrivateKeyError(
          error,
          t("private-keys.errors.revealFailed"),
        ),
      );
    } finally {
      revealingId.value = null;
    }
  }

  function closeReveal(): void {
    showing.value = null;
  }

  async function remove(key: PrivateKey): Promise<void> {
    if (removingId.value !== null) {
      return;
    }
    removingId.value = key.id;
    try {
      await deletePrivateKey(key.id);
      message.success(t("private-keys.toast.deleted"));
      await refresh();
    } catch (error) {
      message.error(
        describePrivateKeyError(
          error,
          t("private-keys.errors.deleteFailed"),
        ),
      );
    } finally {
      removingId.value = null;
    }
  }

  onMounted(() => {
    void refresh();
  });

  return {
    keys,
    loading,
    loaded,
    errorMessage,
    usageById,
    createOpen,
    createTab,
    createName,
    createDescription,
    createType,
    importPrivateKeyValue,
    importPublicKeyValue,
    createTouched,
    createBusy,
    createNameError,
    importPrivateKeyError,
    createErrorMessage,
    typeOptions,
    editing,
    editName,
    editDescription,
    editTouched,
    editBusy,
    editNameError,
    editErrorMessage,
    viewing,
    showing,
    revealingId,
    removingId,
    refresh,
    openCreate,
    closeCreate,
    touchCreate,
    submitCreate,
    openView,
    closeView,
    openEdit,
    closeEdit,
    touchEdit,
    submitEdit,
    reveal,
    closeReveal,
    remove,
  };
}
