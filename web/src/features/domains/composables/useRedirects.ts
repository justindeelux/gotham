import { computed, inject, provide, ref } from "vue";
import type { InjectionKey } from "vue";
import { useMessage } from "naive-ui";
import { activeLocale } from "@/shared/i18n";

import { describeProxyError, proxyText } from "@/features/domains/api/proxy";
import type { DomainRedirect, RedirectCode } from "@/features/domains/api/proxy";
import { useProxyStore } from "@/features/domains/stores/proxy";

export interface RedirectForm {
  application_id: string;
  source_domain: string;
  target_domain: string;
  code: RedirectCode;
  preserve_path: boolean;
  enabled: boolean;
}

export function emptyRedirectForm(): RedirectForm {
  return {
    application_id: "",
    source_domain: "",
    target_domain: "",
    code: 301,
    preserve_path: true,
    enabled: true,
  };
}

export function redirectCodeOptions(): Array<{
  label: string;
  value: RedirectCode;
}> {
  return [
    {
      label: proxyText(
        "domains.redirects.codeOption",
        "{code} · {kind}",
        {
          code: 301,
          kind: proxyText("domains.redirects.codePermanent", "permanent"),
        },
      ),
      value: 301,
    },
    {
      label: proxyText(
        "domains.redirects.codeOption",
        "{code} · {kind}",
        {
          code: 302,
          kind: proxyText("domains.redirects.codeTemporary", "temporary"),
        },
      ),
      value: 302,
    },
  ];
}

function createRedirectsState() {
  const message = useMessage();
  const proxyStore = useProxyStore();

  const redirectForm = ref<RedirectForm>(emptyRedirectForm());
  const redirectSaving = ref(false);
  const redirectErrorRaw = ref<unknown>(null);
  const redirectError = computed<string | null>(() => {
    void activeLocale.value;
    return redirectErrorRaw.value === null
      ? null
      : describeProxyError(redirectErrorRaw.value);
  });
  const editingRedirect = ref<DomainRedirect | null>(null);
  const redirectEditOpen = ref(false);
  const redirectEditSaving = ref(false);
  const redirectEditErrorRaw = ref<unknown>(null);
  const redirectEditError = computed<string | null>(() => {
    void activeLocale.value;
    return redirectEditErrorRaw.value === null
      ? null
      : describeProxyError(redirectEditErrorRaw.value);
  });
  const redirectEditForm = ref<RedirectForm>(emptyRedirectForm());

  const enabledRedirects = computed<number>(
    () => proxyStore.redirects.filter((item) => item.enabled).length,
  );

  const applicationOptions = computed(() =>
    proxyStore.applications.map((application) => ({
      label: application.primary_domain
        ? `${application.name} · ${application.primary_domain}`
        : proxyText(
            "domains.redirects.appWithoutDomain",
            "{name} · no base domain",
            { name: application.name },
          ),
      value: application.id,
    })),
  );

  function openRedirectEdit(redirect: DomainRedirect): void {
    editingRedirect.value = redirect;
    redirectEditForm.value = {
      application_id: redirect.application_id,
      source_domain: redirect.source_domain,
      target_domain: redirect.target_domain,
      code: redirect.code,
      preserve_path: redirect.preserve_path,
      enabled: redirect.enabled,
    };
    redirectEditErrorRaw.value = null;
    redirectEditOpen.value = true;
  }

  async function handleCreateRedirect(): Promise<void> {
    redirectErrorRaw.value = null;
    redirectSaving.value = true;
    try {
      const form = redirectForm.value;
      await proxyStore.createRedirectRule({
        application_id: form.application_id,
        source_domain: form.source_domain,
        target_domain: form.target_domain,
        code: form.code,
        preserve_path: form.preserve_path,
        enabled: form.enabled,
      });
      message.success(
        proxyText("domains.redirects.created", "Redirect rule created."),
      );
      redirectForm.value = {
        ...emptyRedirectForm(),
        application_id: form.application_id,
      };
    } catch (error) {
      redirectErrorRaw.value = error;
    } finally {
      redirectSaving.value = false;
    }
  }

  async function handleSaveRedirect(): Promise<void> {
    const existing = editingRedirect.value;
    if (!existing) {
      return;
    }
    redirectEditErrorRaw.value = null;
    redirectEditSaving.value = true;
    try {
      const form = redirectEditForm.value;
      await proxyStore.updateRedirectRule(existing.id, {
        source_domain: form.source_domain,
        target_domain: form.target_domain,
        code: form.code,
        preserve_path: form.preserve_path,
        enabled: form.enabled,
      });
      message.success(
        proxyText("domains.redirects.saved", "Redirect rule saved."),
      );
      redirectEditOpen.value = false;
    } catch (error) {
      redirectEditErrorRaw.value = error;
    } finally {
      redirectEditSaving.value = false;
    }
  }

  async function handleToggleRedirect(
    redirect: DomainRedirect,
    enabled: boolean,
  ): Promise<void> {
    try {
      await proxyStore.updateRedirectRule(redirect.id, { enabled });
      message.success(
        enabled
          ? proxyText("domains.redirects.enabledToast", "Redirect rule enabled.")
          : proxyText("domains.redirects.pausedToast", "Redirect rule paused."),
      );
    } catch (error) {
      message.error(describeProxyError(error));
    }
  }

  async function handleDeleteRedirect(redirect: DomainRedirect): Promise<void> {
    try {
      await proxyStore.removeRedirectRule(redirect.id);
      message.success(
        proxyText("domains.redirects.deleted", "Redirect rule deleted."),
      );
    } catch (error) {
      message.error(describeProxyError(error));
    }
  }

  return {
    redirectForm,
    redirectSaving,
    redirectError,
    editingRedirect,
    redirectEditOpen,
    redirectEditSaving,
    redirectEditError,
    redirectEditForm,
    enabledRedirects,
    applicationOptions,
    openRedirectEdit,
    handleCreateRedirect,
    handleSaveRedirect,
    handleToggleRedirect,
    handleDeleteRedirect,
  };
}

export type RedirectsState = ReturnType<typeof createRedirectsState>;

const redirectsKey: InjectionKey<RedirectsState> = Symbol("domains.redirects");

export function provideRedirects(): RedirectsState {
  const state = createRedirectsState();
  provide(redirectsKey, state);
  return state;
}

export function useRedirects(): RedirectsState {
  const state = inject(redirectsKey);
  if (!state) {
    throw new Error("useRedirects must be used inside a page providing it.");
  }
  return state;
}
