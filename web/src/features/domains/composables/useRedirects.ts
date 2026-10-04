import { computed, inject, provide, ref } from "vue";
import type { InjectionKey } from "vue";
import { useMessage } from "naive-ui";

import { describeProxyError } from "@/features/domains/api/proxy";
import type { DomainRedirect, RedirectCode } from "@/features/domains/api/proxy";
import { useProxyStore } from "@/features/domains/stores/proxy";

/** Redirect create/edit form state. */
export interface RedirectForm {
  application_id: string;
  source_domain: string;
  target_domain: string;
  code: RedirectCode;
  preserve_path: boolean;
  enabled: boolean;
}

/** emptyRedirectForm returns a create-mode redirect draft. */
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

export const redirectCodeOptions = [
  { label: "301 · permanent", value: 301 },
  { label: "302 · temporary", value: 302 },
];

/**
 * Redirect state is per page instance (created by provideRedirects in the
 * page, shared via inject): drafts and dialog flags never outlive the page.
 */
function createRedirectsState() {
  const message = useMessage();
  const proxyStore = useProxyStore();

  const redirectForm = ref<RedirectForm>(emptyRedirectForm());
  const redirectSaving = ref(false);
  const redirectError = ref<string | null>(null);
  const editingRedirect = ref<DomainRedirect | null>(null);
  const redirectEditOpen = ref(false);
  const redirectEditSaving = ref(false);
  const redirectEditError = ref<string | null>(null);
  const redirectEditForm = ref<RedirectForm>(emptyRedirectForm());

  const enabledRedirects = computed<number>(
    () => proxyStore.redirects.filter((item) => item.enabled).length,
  );

  const applicationOptions = computed(() =>
    proxyStore.applications.map((application) => ({
      label: application.base_domain
        ? `${application.name} · ${application.base_domain}`
        : `${application.name} · no base domain`,
      value: application.id,
    })),
  );

  /** openRedirectEdit seeds the dialog from a stored rule. */
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
    redirectEditError.value = null;
    redirectEditOpen.value = true;
  }

  /** handleCreateRedirect stores one rule from the add-redirect form. */
  async function handleCreateRedirect(): Promise<void> {
    redirectError.value = null;
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
      message.success("Redirect rule created.");
      // Keep the application so a second rule can be added quickly.
      redirectForm.value = {
        ...emptyRedirectForm(),
        application_id: form.application_id,
      };
    } catch (error) {
      redirectError.value = describeProxyError(error);
    } finally {
      redirectSaving.value = false;
    }
  }

  /** handleSaveRedirect patches one rule from the edit dialog. */
  async function handleSaveRedirect(): Promise<void> {
    const existing = editingRedirect.value;
    if (!existing) {
      return;
    }
    redirectEditError.value = null;
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
      message.success("Redirect rule saved.");
      redirectEditOpen.value = false;
    } catch (error) {
      redirectEditError.value = describeProxyError(error);
    } finally {
      redirectEditSaving.value = false;
    }
  }

  /** handleToggleRedirect enables or pauses one rule. */
  async function handleToggleRedirect(
    redirect: DomainRedirect,
    enabled: boolean,
  ): Promise<void> {
    try {
      await proxyStore.updateRedirectRule(redirect.id, { enabled });
      message.success(enabled ? "Redirect rule enabled." : "Redirect rule paused.");
    } catch (error) {
      message.error(describeProxyError(error));
    }
  }

  /** handleDeleteRedirect removes one rule. */
  async function handleDeleteRedirect(redirect: DomainRedirect): Promise<void> {
    try {
      await proxyStore.removeRedirectRule(redirect.id);
      message.success("Redirect rule deleted.");
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

/**
 * provideRedirects creates the redirect state for one page mount.
 * Call once in the page; descendants share it through useRedirects.
 */
export function provideRedirects(): RedirectsState {
  const state = createRedirectsState();
  provide(redirectsKey, state);
  return state;
}

/** useRedirects shares the page instance; call in descendant components. */
export function useRedirects(): RedirectsState {
  const state = inject(redirectsKey);
  if (!state) {
    throw new Error("useRedirects must be used inside a page providing it.");
  }
  return state;
}
