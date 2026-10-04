import { useMessage } from "naive-ui";
import { ref } from "vue";

import { describeProxyError } from "@/features/domains/api/proxy";
import type { DNSProvider, DNSProviderName } from "@/features/domains/api/proxy";
import { useProxyStore } from "@/features/domains/stores/proxy";

/** Provider dialog state. */
export interface ProviderForm {
  provider: DNSProviderName;
  name: string;
  zones: string[];
  credential: string;
  enabled: boolean;
}

/** Dialog state is module-scoped: the page head, the providers panel and the dialog share it. */
const providerOpen = ref(false);
const providerSaving = ref(false);
const providerError = ref<string | null>(null);
const editingProvider = ref<DNSProvider | null>(null);
const providerForm = ref<ProviderForm>(emptyProviderForm());

/** emptyProviderForm returns a create-mode provider draft. */
export function emptyProviderForm(): ProviderForm {
  return {
    provider: "cloudflare",
    name: "",
    zones: [],
    credential: "",
    enabled: true,
  };
}

/** useProviders owns the DNS provider dialog, toggles and deletions. */
export function useProviders() {
  const message = useMessage();
  const proxyStore = useProxyStore();

  /**
   * clearProviderCredential drops the plaintext API token from component
   * memory. The credential is write-only: it is cleared after a successful
   * write and on after-leave, which covers Cancel, the close icon, Escape, the
   * mask and a failed write followed by dismissal.
   */
  function clearProviderCredential(): void {
    providerForm.value = { ...providerForm.value, credential: "" };
  }

  /**
   * openProviderCreate resets the dialog for a new provider. The credential
   * field is deliberately not touched: after-leave (every dismissal path) and a
   * successful write are the only clearing points, so it is blank whenever the
   * dialog can be opened again — the smoke asserts exactly that.
   */
  function openProviderCreate(): void {
    editingProvider.value = null;
    providerForm.value = {
      ...providerForm.value,
      provider: "cloudflare",
      name: "",
      zones: [],
      enabled: true,
    };
    providerError.value = null;
    providerOpen.value = true;
  }

  /**
   * openProviderEdit seeds the dialog from a stored provider. The credential
   * field keeps its current (blank, see openProviderCreate) value; empty means
   * "keep the stored credential" and is never read back.
   */
  function openProviderEdit(provider: DNSProvider): void {
    editingProvider.value = provider;
    providerForm.value = {
      ...providerForm.value,
      provider: provider.provider,
      name: provider.name,
      zones: [...provider.zones],
      enabled: provider.enabled,
    };
    providerError.value = null;
    providerOpen.value = true;
  }

  /** handleSaveProvider creates or patches one DNS provider. */
  async function handleSaveProvider(): Promise<void> {
    providerError.value = null;
    providerSaving.value = true;
    try {
      const form = providerForm.value;
      const existing = editingProvider.value;
      if (existing) {
        await proxyStore.updateProvider(existing.id, {
          provider: form.provider,
          name: form.name,
          zones: form.zones,
          // Omit an untouched credential so the stored one stays sealed as-is.
          ...(form.credential !== "" ? { credential: form.credential } : {}),
          enabled: form.enabled,
        });
        message.success("DNS provider saved.");
      } else {
        await proxyStore.createProvider({
          provider: form.provider,
          name: form.name,
          zones: form.zones,
          credential: form.credential,
          enabled: form.enabled,
        });
        message.success("DNS provider created.");
      }
      // The plaintext token must not outlive the write (or the dialog).
      clearProviderCredential();
      providerOpen.value = false;
    } catch (error) {
      providerError.value = describeProxyError(error);
    } finally {
      providerSaving.value = false;
    }
  }

  /** handleToggleProvider enables or disables a provider. */
  async function handleToggleProvider(
    provider: DNSProvider,
    enabled: boolean,
  ): Promise<void> {
    try {
      await proxyStore.updateProvider(provider.id, { enabled });
      message.success(enabled ? "Provider enabled." : "Provider disabled.");
    } catch (error) {
      message.error(describeProxyError(error));
      // The store list still holds the server state after the refresh the
      // failed write did not perform; reload explicitly.
      void proxyStore.fetchProviders();
    }
  }

  /** handleDeleteProvider removes a provider that nothing references. */
  async function handleDeleteProvider(provider: DNSProvider): Promise<void> {
    try {
      await proxyStore.removeProvider(provider.id);
      message.success("DNS provider deleted.");
    } catch (error) {
      message.error(describeProxyError(error));
    }
  }

  return {
    providerOpen,
    providerSaving,
    providerError,
    editingProvider,
    providerForm,
    clearProviderCredential,
    openProviderCreate,
    openProviderEdit,
    handleSaveProvider,
    handleToggleProvider,
    handleDeleteProvider,
  };
}
