import { computed, inject, provide, ref } from "vue";
import type { InjectionKey } from "vue";
import { useMessage } from "naive-ui";
import { activeLocale } from "@/shared/i18n";

import { describeProxyError, proxyText } from "@/features/domains/api/proxy";
import type { DNSProvider, DNSProviderName } from "@/features/domains/api/proxy";
import { useProxyStore } from "@/features/domains/stores/proxy";

export interface ProviderForm {
  provider: DNSProviderName;
  name: string;
  zones: string[];
  credential: string;
  enabled: boolean;
}

export function emptyProviderForm(): ProviderForm {
  return {
    provider: "cloudflare",
    name: "",
    zones: [],
    credential: "",
    enabled: true,
  };
}

function createProvidersState() {
  const message = useMessage();
  const proxyStore = useProxyStore();

  const providerOpen = ref(false);
  const providerSaving = ref(false);
  const providerErrorRaw = ref<unknown>(null);
  const providerError = computed<string | null>(() => {
    void activeLocale.value;
    return providerErrorRaw.value === null
      ? null
      : describeProxyError(providerErrorRaw.value);
  });
  const editingProvider = ref<DNSProvider | null>(null);
  const providerForm = ref<ProviderForm>(emptyProviderForm());

  function clearProviderCredential(): void {
    providerForm.value = { ...providerForm.value, credential: "" };
  }

  function openProviderCreate(): void {
    editingProvider.value = null;
    providerForm.value = {
      ...providerForm.value,
      provider: "cloudflare",
      name: "",
      zones: [],
      enabled: true,
    };
    providerErrorRaw.value = null;
    providerOpen.value = true;
  }

  function openProviderEdit(provider: DNSProvider): void {
    editingProvider.value = provider;
    providerForm.value = {
      ...providerForm.value,
      provider: provider.provider,
      name: provider.name,
      zones: [...provider.zones],
      enabled: provider.enabled,
    };
    providerErrorRaw.value = null;
    providerOpen.value = true;
  }

  async function handleSaveProvider(): Promise<void> {
    providerErrorRaw.value = null;
    providerSaving.value = true;
    try {
      const form = providerForm.value;
      const existing = editingProvider.value;
      if (existing) {
        await proxyStore.updateProvider(existing.id, {
          provider: form.provider,
          name: form.name,
          zones: form.zones,
          ...(form.credential !== "" ? { credential: form.credential } : {}),
          enabled: form.enabled,
        });
        message.success(proxyText("domains.providers.saved", "DNS provider saved."));
      } else {
        await proxyStore.createProvider({
          provider: form.provider,
          name: form.name,
          zones: form.zones,
          credential: form.credential,
          enabled: form.enabled,
        });
        message.success(proxyText("domains.providers.created", "DNS provider created."));
      }
      clearProviderCredential();
      providerOpen.value = false;
    } catch (error) {
      providerErrorRaw.value = error;
    } finally {
      providerSaving.value = false;
    }
  }

  async function handleToggleProvider(
    provider: DNSProvider,
    enabled: boolean,
  ): Promise<void> {
    try {
      await proxyStore.updateProvider(provider.id, { enabled });
      message.success(
        enabled
          ? proxyText("domains.providers.enabledToast", "Provider enabled.")
          : proxyText("domains.providers.disabledToast", "Provider disabled."),
      );
    } catch (error) {
      message.error(describeProxyError(error));
      void proxyStore.fetchProviders();
    }
  }

  async function handleDeleteProvider(provider: DNSProvider): Promise<void> {
    try {
      await proxyStore.removeProvider(provider.id);
      message.success(proxyText("domains.providers.deleted", "DNS provider deleted."));
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

export type ProvidersState = ReturnType<typeof createProvidersState>;

const providersKey: InjectionKey<ProvidersState> = Symbol("domains.providers");

export function provideProviders(): ProvidersState {
  const state = createProvidersState();
  provide(providersKey, state);
  return state;
}

export function useProviders(): ProvidersState {
  const state = inject(providersKey);
  if (!state) {
    throw new Error("useProviders must be used inside a page providing it.");
  }
  return state;
}
