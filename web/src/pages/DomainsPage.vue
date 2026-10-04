<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h, onMounted, ref } from "vue";
import type { VNode } from "vue";
import { RouterLink } from "vue-router";

import {
  certificateStatusLabel,
  certificateStatusTagType,
  describeProxyError,
  draftFromCertificate,
  providerLabel,
  toCertificateInput,
} from "../api/proxy";
import type {
  Certificate,
  CertificateDraft,
  DNSProvider,
  DNSProviderName,
  DomainRedirect,
  RedirectCode,
} from "../api/proxy";
import CertificateForm from "../components/CertificateForm.vue";
import { useProxyStore } from "../stores/proxy";
import { expiryLabel, formatDate, relativeTime } from "../utils/format";

/**
 * Domains & SSL page: DNS provider credentials, certificate configurations
 * and domain redirect rules, all backed by the proxy API. Certificate
 * status/expiry is observed live from the owning node; a router listing still
 * has no API and is rendered as an explicitly labeled stub — never with
 * invented data.
 */

const message = useMessage();
const proxyStore = useProxyStore();

const activeTab = ref("certificates");

/** Provider dialog state. */
interface ProviderForm {
  provider: DNSProviderName;
  name: string;
  zones: string[];
  credential: string;
  enabled: boolean;
}

const providerOpen = ref(false);
const providerSaving = ref(false);
const providerError = ref<string | null>(null);
const editingProvider = ref<DNSProvider | null>(null);
const providerForm = ref<ProviderForm>(emptyProviderForm());

/** Certificate dialog state. */
const certificateOpen = ref(false);
const certificateSaving = ref(false);
const certificateError = ref<string | null>(null);
const editingCertificate = ref<Certificate | null>(null);
const certificateDraft = ref<CertificateDraft>(emptyCertificateDraft());

/** Redirect create form and edit dialog state. */
interface RedirectForm {
  application_id: string;
  source_domain: string;
  target_domain: string;
  code: RedirectCode;
  preserve_path: boolean;
  enabled: boolean;
}

const redirectForm = ref<RedirectForm>(emptyRedirectForm());
const redirectSaving = ref(false);
const redirectError = ref<string | null>(null);
const editingRedirect = ref<DomainRedirect | null>(null);
const redirectEditOpen = ref(false);
const redirectEditSaving = ref(false);
const redirectEditError = ref<string | null>(null);
const redirectEditForm = ref<RedirectForm>(emptyRedirectForm());

/** emptyRedirectForm returns a create-mode redirect draft. */
function emptyRedirectForm(): RedirectForm {
  return {
    application_id: "",
    source_domain: "",
    target_domain: "",
    code: 301,
    preserve_path: true,
    enabled: true,
  };
}

/** emptyProviderForm returns a create-mode provider draft. */
function emptyProviderForm(): ProviderForm {
  return {
    provider: "cloudflare",
    name: "",
    zones: [],
    credential: "",
    enabled: true,
  };
}

/** emptyCertificateDraft returns a create-mode certificate draft. */
function emptyCertificateDraft(): CertificateDraft {
  return {
    application_id: "",
    challenge: "http-01",
    dns_provider_id: "",
    wildcard: false,
    enabled: true,
  };
}

/**
 * clearProviderCredential drops the plaintext API token from component
 * memory. The credential is write-only: it is cleared after a successful
 * write and on after-leave, which covers Cancel, the close icon, Escape, the
 * mask and a failed write followed by dismissal.
 */
function clearProviderCredential(): void {
  providerForm.value = { ...providerForm.value, credential: "" };
}

const providerTypeOptions = [
  { label: "Cloudflare", value: "cloudflare" },
  { label: "DigitalOcean", value: "digitalocean" },
];

const enabledProviders = computed<DNSProvider[]>(() =>
  proxyStore.providers.filter((provider) => provider.enabled),
);

const applicationsWithDomain = computed<number>(
  () => proxyStore.applications.filter((item) => item.base_domain !== "").length,
);

const enabledCertificates = computed<number>(
  () => proxyStore.certificates.filter((item) => item.enabled).length,
);

const wildcardCertificates = computed<number>(
  () => proxyStore.certificates.filter((item) => item.wildcard).length,
);

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

const redirectCodeOptions = [
  { label: "301 · permanent", value: 301 },
  { label: "302 · temporary", value: 302 },
];

/** providerName resolves a provider id to its display label. */
function providerName(providerId: string): string {
  if (!providerId) {
    return "—";
  }
  const provider = proxyStore.providerOf(providerId);
  return provider
    ? provider.name || providerLabel(provider.provider)
    : "unknown provider";
}

/** applicationName resolves an application id to its display name. */
function applicationName(applicationId: string): string {
  return proxyStore.applicationOf(applicationId)?.name ?? applicationId.slice(0, 8);
}

/** applicationDomain shows the base domain a certificate targets. */
function applicationDomain(certificate: Certificate): string {
  return proxyStore.applicationOf(certificate.application_id)?.base_domain ?? "";
}

/** domainCell renders the recorded domain with its application. */
function domainCell(certificate: Certificate): VNode {
  const domain = applicationDomain(certificate);
  return h("div", { class: "cell-main" }, [
    h("span", { class: "mono cell-name" }, certificate.domain),
    h(
      "span",
      { class: "cell-sub" },
      domain && domain !== certificate.domain
        ? `${applicationName(certificate.application_id)} · base domain ` +
          `changed to ${domain} — re-save to re-record`
        : applicationName(certificate.application_id),
    ),
  ]);
}

/** statusCell renders the node-observed certificate status. */
function statusCell(certificate: Certificate): VNode {
  return h(
    NTag,
    {
      size: "small",
      type: certificateStatusTagType(certificate.status),
    },
    { default: () => certificateStatusLabel(certificate.status) },
  );
}

/** expiryCell renders the live expiry, only ever for a present certificate. */
function expiryCell(certificate: Certificate): VNode {
  if (certificate.status !== "present") {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  if (!certificate.not_after) {
    return h(NText, { depth: 3 }, { default: () => "not reported" });
  }
  return h("div", { class: "cell-main" }, [
    h("span", { class: "mono" }, formatDate(certificate.not_after)),
    h("span", { class: "cell-sub" }, expiryLabel(certificate.not_after)),
  ]);
}

/** actionsCell renders Edit / Delete controls for one certificate. */
function certificateActions(certificate: Certificate): VNode {
  return h(NSpace, { size: 8, align: "center", wrap: false }, {
    default: () => [
      h(
        NButton,
        { size: "small", onClick: () => openCertificateEdit(certificate) },
        { default: () => "Edit" },
      ),
      h(
        NPopconfirm,
        {
          positiveButtonProps: { type: "error" },
          onPositiveClick: () => void handleDeleteCertificate(certificate),
        },
        {
          trigger: () =>
            h(
              NButton,
              { size: "small", type: "error", ghost: true },
              { default: () => "Delete" },
            ),
          default: () =>
            `Delete the certificate configuration for ${certificate.domain}? ` +
            "The route falls back to plain HTTP.",
        },
      ),
    ],
  });
}

const certificateColumns: DataTableColumns<Certificate> = [
  {
    title: "Domain",
    key: "domain",
    minWidth: 240,
    render: (row) => domainCell(row),
  },
  {
    title: "Challenge",
    key: "challenge",
    width: 110,
    render: (row) => h("span", { class: "mono" }, row.challenge),
  },
  {
    title: "DNS provider",
    key: "dns_provider_id",
    minWidth: 160,
    render: (row) => h("span", { class: "mono" }, providerName(row.dns_provider_id)),
  },
  {
    title: "Wildcard",
    key: "wildcard",
    width: 110,
    render: (row) =>
      row.wildcard
        ? h(NTag, { size: "small" }, { default: () => "wildcard" })
        : h(NText, { depth: 3 }, { default: () => "no" }),
  },
  {
    title: "Enabled",
    key: "enabled",
    width: 110,
    render: (row) =>
      h(
        NTag,
        { size: "small", type: row.enabled ? "success" : "default" },
        { default: () => (row.enabled ? "enabled" : "disabled") },
      ),
  },
  {
    title: "Status",
    key: "status",
    width: 150,
    render: (row) => statusCell(row),
  },
  {
    title: "Expires",
    key: "not_after",
    minWidth: 180,
    render: (row) => expiryCell(row),
  },
  {
    title: "Updated",
    key: "updated_at",
    width: 120,
    render: (row) => relativeTime(row.updated_at),
  },
  {
    title: "Actions",
    key: "actions",
    width: 160,
    render: (row) => certificateActions(row),
  },
];

/** rowKey identifies a certificate row by its id. */
function certificateRowKey(row: Certificate): string {
  return row.id;
}

/**
 * statsReady flips once the first load settles, so the tiles never flash a
 * 0 that was never read.
 */
const statsReady = ref(false);

/** load refreshes providers, certificates, redirects and the name map. */
async function load(): Promise<void> {
  try {
    await Promise.allSettled([
      proxyStore.fetchProviders(),
      proxyStore.fetchCertificates(),
      proxyStore.fetchRedirects(),
      proxyStore.fetchApplications(),
    ]);
  } finally {
    statsReady.value = true;
  }
}

/**
 * statsBlocked is true while the first load is in flight or when a read
 * failed (for example a non-admin 403): the tiles then show a dash rather
 * than a 0 that looks like real data.
 */
const statsBlocked = computed<boolean>(
  () =>
    !statsReady.value ||
    proxyStore.error !== null ||
    proxyStore.certificatesError !== null,
);

/** statText renders a tile count, or a dash when the read failed/is pending. */
function statText(count: number): string {
  return statsBlocked.value ? "—" : String(count);
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

/** openCertificateCreate resets the dialog for a new configuration. */
function openCertificateCreate(): void {
  editingCertificate.value = null;
  certificateDraft.value = emptyCertificateDraft();
  certificateError.value = null;
  certificateOpen.value = true;
}

/** openCertificateEdit seeds the dialog from a stored configuration. */
function openCertificateEdit(certificate: Certificate): void {
  editingCertificate.value = certificate;
  certificateDraft.value = draftFromCertificate(certificate);
  certificateError.value = null;
  certificateOpen.value = true;
}

/** handleSaveCertificate creates or patches one certificate configuration. */
async function handleSaveCertificate(): Promise<void> {
  certificateError.value = null;
  certificateSaving.value = true;
  try {
    const input = toCertificateInput(certificateDraft.value);
    const existing = editingCertificate.value;
    if (existing) {
      await proxyStore.updateCertificateConfig(existing.id, input);
      message.success("Certificate configuration saved.");
    } else {
      await proxyStore.createCertificateConfig({
        ...input,
        application_id: certificateDraft.value.application_id,
      });
      message.success("Certificate configuration created.");
    }
    certificateOpen.value = false;
  } catch (error) {
    certificateError.value = describeProxyError(error);
  } finally {
    certificateSaving.value = false;
  }
}

/** handleDeleteCertificate removes one configuration. */
async function handleDeleteCertificate(certificate: Certificate): Promise<void> {
  try {
    await proxyStore.removeCertificate(certificate.id);
    message.success("Certificate configuration deleted.");
  } catch (error) {
    message.error(describeProxyError(error));
  }
}

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

onMounted(() => {
  void load();
});
</script>

<template>
  <div class="domains-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Operations · Reverse proxy</p>
        <h1>Domains &amp; SSL</h1>
        <p class="page-desc">
          Traefik 3.1 runs on every node as the container
          <span class="mono">gotham-traefik</span>. The control plane generates
          the dynamic configuration from application state through the file
          provider — testable, idempotent, and keeping one previous version for
          a quick rollback. Certificates below are intent records; the node
          performs ACME issuance.
        </p>
      </div>
      <div class="page-actions">
        <NButton @click="openProviderCreate">Add DNS provider</NButton>
        <NButton type="primary" @click="openCertificateCreate">
          Add certificate
        </NButton>
      </div>
    </div>

    <NAlert
      v-if="proxyStore.error"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ proxyStore.error }}
    </NAlert>

    <div class="grid cols-4 kpi-row">
      <div class="stat">
        <p class="stat-label">Certificate configs</p>
        <p class="stat-value num">{{ statText(proxyStore.certificates.length) }}</p>
        <p class="stat-sub">{{ statText(enabledCertificates) }} enabled · one per application</p>
      </div>
      <div class="stat">
        <p class="stat-label">Wildcard configs</p>
        <p class="stat-value num">{{ statText(wildcardCertificates) }}</p>
        <p class="stat-sub">dns-01 challenge required</p>
      </div>
      <div class="stat">
        <p class="stat-label">DNS providers</p>
        <p class="stat-value num">{{ statText(proxyStore.providers.length) }}</p>
        <p class="stat-sub">{{ statText(enabledProviders.length) }} enabled · credential sealed</p>
      </div>
      <div class="stat">
        <p class="stat-label">Applications with a domain</p>
        <p class="stat-value num">{{ statText(applicationsWithDomain) }}</p>
        <p class="stat-sub">edited on each application page</p>
      </div>
    </div>

    <NTabs v-model:value="activeTab" type="line" animated class="tabs">
      <NTabPane name="routers" tab="Routers">
        <NCard style="margin-top: 16px" title="Router list — backend pending">
          <NEmpty description="The generated Traefik routers are not exposed by the API yet.">
            <template #extra>
              <p class="empty-hint">
                The control plane generates the file-provider configuration from
                application state (BE-6.1), but there is no read endpoint for the
                resulting routers. A live router table arrives in a later backend
                package; nothing is shown here rather than invented.
              </p>
              <RouterLink :to="{ name: 'applications' }">
                Manage application domains
              </RouterLink>
            </template>
          </NEmpty>
        </NCard>
      </NTabPane>

      <NTabPane name="certificates" tab="Certificates">
        <NCard style="margin-top: 16px" title="Certificate configurations">
          <template #header-extra>
            <NText depth="3">one per application</NText>
          </template>
          <NSpace vertical :size="12">
            <NAlert
              v-if="proxyStore.certificatesError"
              type="error"
              :show-icon="true"
            >
              {{ proxyStore.certificatesError }}
            </NAlert>
            <NDataTable
              v-if="proxyStore.certificates.length > 0 || proxyStore.certificatesLoading"
              :columns="certificateColumns"
              :data="proxyStore.certificates"
              :loading="proxyStore.certificatesLoading"
              :row-key="certificateRowKey"
              :bordered="false"
              :scroll-x="1000"
              :pagination="{ pageSize: 10 }"
            />
            <NEmpty v-else description="No certificate configurations yet.">
              <template #extra>
                <NButton type="primary" @click="openCertificateCreate">
                  Add certificate
                </NButton>
              </template>
            </NEmpty>
            <div class="embed">
              <h4>Status is observed live from the owning node</h4>
              <p>
                <span class="mono">present</span> means the node's ACME storage
                holds a certificate for the recorded domain and its expiry is
                shown; <span class="mono">absent</span> means the storage was
                read and holds none; <span class="mono">unknown</span> means the
                node could not be read. The status is computed on read — the
                control plane stores the desired configuration only.
              </p>
            </div>
          </NSpace>
          <template #footer>
            <NText depth="3">
              The recorded domain comes from the application's base domain at
              save time. Changing the base domain later requires saving the
              configuration again to re-record it.
            </NText>
          </template>
        </NCard>
      </NTabPane>

      <NTabPane name="dns" tab="DNS providers">
        <NCard style="margin-top: 16px" title="DNS providers">
          <template #header-extra>
            <NButton size="small" @click="openProviderCreate">
              Add provider
            </NButton>
          </template>
          <NSpace vertical :size="12">
            <NText depth="3">
              Credentials are sealed server-side and never returned by the API.
              The UI can set or rotate a credential, but cannot display or copy
              it.
            </NText>
            <div
              v-if="proxyStore.providers.length > 0"
              class="grid cols-2"
            >
              <div
                v-for="provider in proxyStore.providers"
                :key="provider.id"
                class="channel-card"
              >
                <span class="channel-mark mono">
                  {{ provider.provider === "cloudflare" ? "CF" : "DO" }}
                </span>
                <div class="channel-body">
                  <div class="channel-head">
                    <h4>{{ provider.name || providerLabel(provider.provider) }}</h4>
                    <NTag size="small">{{ provider.provider }}</NTag>
                    <NTag
                      size="small"
                      :type="provider.enabled ? 'success' : 'default'"
                    >
                      {{ provider.enabled ? "enabled" : "disabled" }}
                    </NTag>
                  </div>
                  <dl class="kv">
                    <dt>Zones</dt>
                    <dd class="mono">{{ provider.zones.join(", ") }}</dd>
                    <dt>Credential</dt>
                    <dd class="mono">
                      {{ provider.credentials_set ? "set" : "not set" }} · the API
                      never returns the token
                    </dd>
                    <dt>Updated</dt>
                    <dd>{{ relativeTime(provider.updated_at) }}</dd>
                  </dl>
                  <NSpace :size="8" align="center" class="channel-actions">
                    <NSwitch
                      :value="provider.enabled"
                      :aria-label="`Enable ${provider.name || provider.provider}`"
                      @update:value="(value: boolean) => handleToggleProvider(provider, value)"
                    />
                    <NButton size="small" @click="openProviderEdit(provider)">
                      Edit &amp; rotate credential
                    </NButton>
                    <NPopconfirm
                      :positive-button-props="{ type: 'error' }"
                      @positive-click="handleDeleteProvider(provider)"
                    >
                      <template #trigger>
                        <NButton size="small" type="error" ghost>
                          Delete
                        </NButton>
                      </template>
                      Delete the {{ provider.provider }} provider
                      {{ provider.name || "" }}? Providers referenced by a
                      certificate configuration cannot be deleted.
                    </NPopconfirm>
                  </NSpace>
                </div>
              </div>
            </div>
            <NEmpty
              v-else
              description="No DNS providers configured."
            >
              <template #extra>
                <p class="empty-hint">
                  DNS-01 challenges need a provider credential. Wildcard
                  certificates require the DNS-01 challenge.
                </p>
                <NButton type="primary" @click="openProviderCreate">
                  Add provider
                </NButton>
              </template>
            </NEmpty>
            <div class="embed embed--warn">
              <h4>DNS-01 requires the zone to be delegated to the provider</h4>
              <p>
                Let's Encrypt validates through a TXT record
                <span class="mono">_acme-challenge.&lt;domain&gt;</span> created
                by the provider. If the zone's nameservers do not point at
                Cloudflare or DigitalOcean, the order fails with NXDOMAIN. The
                control plane stores the configured zones; it does not check
                delegation for you.
              </p>
            </div>
          </NSpace>
        </NCard>
      </NTabPane>

      <NTabPane name="redirects" tab="Redirects">
        <NCard style="margin-top: 16px" title="Redirect rules">
          <template #header-extra>
            <span class="card-tag mono">middleware redirectregex</span>
          </template>
          <NSpace vertical :size="12">
            <NAlert v-if="proxyStore.redirectsError" type="error" :show-icon="true">
              {{ proxyStore.redirectsError }}
            </NAlert>
            <div v-if="proxyStore.redirects.length > 0" class="redirect-rows">
              <div
                v-for="redirect in proxyStore.redirects"
                :key="redirect.id"
                class="domain-row"
              >
                <div class="redirect-route">
                  <span class="mono cell-name">{{ redirect.source_domain }}</span>
                  <span class="redirect-arrow" aria-hidden="true">→</span>
                  <span class="mono cell-sub">{{ redirect.target_domain }}</span>
                </div>
                <div class="redirect-code">
                  <NTag size="small">{{ redirect.code }}</NTag>
                  <span class="cell-sub">
                    {{ redirect.code === 301 ? "permanent" : "temporary" }}
                  </span>
                </div>
                <div class="redirect-state">
                  <span
                    class="state-dot"
                    :class="redirect.enabled ? 'state-dot--on' : 'state-dot--off'"
                    aria-hidden="true"
                  ></span>
                  <span class="cell-sub">
                    {{ redirect.enabled ? "enabled" : "paused" }}
                  </span>
                  <span v-if="redirect.preserve_path" class="cell-sub">
                    · keeps the path
                  </span>
                </div>
                <NSpace
                  class="redirect-actions"
                  :size="8"
                  align="center"
                  justify="end"
                >
                  <NSwitch
                    :value="redirect.enabled"
                    :aria-label="`Enable redirect ${redirect.source_domain}`"
                    @update:value="(value: boolean) => handleToggleRedirect(redirect, value)"
                  />
                  <NButton size="small" @click="openRedirectEdit(redirect)">
                    Edit
                  </NButton>
                  <NPopconfirm
                    :positive-button-props="{ type: 'error' }"
                    @positive-click="handleDeleteRedirect(redirect)"
                  >
                    <template #trigger>
                      <NButton size="small" type="error" ghost>Delete</NButton>
                    </template>
                    Delete the redirect
                    {{ redirect.source_domain }} → {{ redirect.target_domain }}?
                    Requests to the source stop redirecting.
                  </NPopconfirm>
                </NSpace>
              </div>
            </div>
            <NEmpty
              v-else-if="!proxyStore.redirectsLoading"
              description="No redirect rules yet."
            >
              <template #extra>
                <p class="empty-hint">
                  A rule sends one exact source host to one exact target host.
                  The target must serve its own certificate.
                </p>
              </template>
            </NEmpty>
            <p v-if="proxyStore.redirects.length > 0" class="cell-sub">
              {{ proxyStore.redirects.length }} rule{{
                proxyStore.redirects.length === 1 ? "" : "s"
              }}
              · {{ enabledRedirects }} enabled. GET answers the stored code;
              other methods answer 308/307 so they keep their method.
            </p>
          </NSpace>
          <template #footer>
            <NText depth="3" class="small">
              Rules are applied by Traefik's redirectRegex middleware on the
              next dynamic configuration sync — no redeploy is needed.
            </NText>
          </template>
        </NCard>

        <NCard style="margin-top: 16px" title="Add redirect" class="form-container">
          <template #header-extra>
            <NText depth="3" class="small">
              applies after the next config sync
            </NText>
          </template>
          <NSpace vertical :size="12">
            <NAlert v-if="redirectError" type="error" :show-icon="true">
              {{ redirectError }}
            </NAlert>
            <NForm label-placement="top" :show-feedback="false" class="redirect-form">
              <div class="redirect-form__fields">
                <NFormItem label="Application" class="field-application">
                  <NSelect
                    v-model:value="redirectForm.application_id"
                    :options="applicationOptions"
                    placeholder="Select an application"
                    aria-label="Redirect application"
                  />
                </NFormItem>
                <NFormItem label="Source domain" class="field-source">
                  <NInput
                    v-model:value="redirectForm.source_domain"
                    placeholder="shop.example.com"
                    aria-label="Redirect source domain"
                  />
                </NFormItem>
                <NFormItem label="Target domain" class="field-target">
                  <NInput
                    v-model:value="redirectForm.target_domain"
                    placeholder="storefront.example.com"
                    aria-label="Redirect target domain"
                  />
                </NFormItem>
                <NFormItem label="Redirect code" class="field-code">
                  <NSelect
                    v-model:value="redirectForm.code"
                    :options="redirectCodeOptions"
                    aria-label="Redirect code"
                  />
                </NFormItem>
              </div>
              <div class="redirect-form__bottom">
                <NFormItem label="Preserve path" class="field-preserve">
                  <NSwitch
                    v-model:value="redirectForm.preserve_path"
                    aria-label="Redirect preserve path"
                  />
                </NFormItem>
                <NFormItem label="Enabled" class="field-enabled">
                  <NSwitch
                    v-model:value="redirectForm.enabled"
                    aria-label="Redirect enabled now"
                  />
                </NFormItem>
                <NButton
                  type="primary"
                  :loading="redirectSaving"
                  @click="handleCreateRedirect"
                >
                  Add redirect
                </NButton>
              </div>
            </NForm>
            <NText depth="3" class="small">
              Source and target must differ. The code applies to GET; HEAD
              and every other method answer 308/307, keeping the method.
            </NText>
          </NSpace>
        </NCard>
      </NTabPane>
    </NTabs>

    <NModal
      v-model:show="providerOpen"
      preset="card"
      :title="editingProvider ? 'Edit DNS provider' : 'Add DNS provider'"
      style="width: 560px; max-width: 94vw"
      @after-leave="clearProviderCredential"
    >
      <NSpace vertical :size="12">
        <NAlert v-if="providerError" type="error" :show-icon="true">
          {{ providerError }}
        </NAlert>
        <NForm label-placement="top" :show-feedback="false" class="provider-form form-container">
          <div class="form-row">
            <NFormItem label="Provider" class="field-provider">
              <NSelect
                v-model:value="providerForm.provider"
                :options="providerTypeOptions"
                aria-label="Provider type"
              />
            </NFormItem>
            <NFormItem label="Name" class="field-name">
              <NInput
                v-model:value="providerForm.name"
                placeholder="Optional label, e.g. Production Cloudflare"
                aria-label="Provider name"
              />
            </NFormItem>
          </div>
          <NFormItem label="Zones" class="field-zones">
            <NSelect
              v-model:value="providerForm.zones"
              multiple
              filterable
              tag
              :options="[]"
              placeholder="Type a zone and press Enter"
              aria-label="DNS zones"
            />
          </NFormItem>
          <NFormItem
            :label="editingProvider ? 'Rotate credential (optional)' : 'Credential'"
            class="field-credential"
          >
            <NInput
              v-model:value="providerForm.credential"
              type="password"
              show-password-on="click"
              autocomplete="new-password"
              :placeholder="
                editingProvider
                  ? 'Leave blank to keep the stored credential'
                  : 'API token'
              "
              aria-label="Provider credential"
            />
          </NFormItem>
          <NFormItem label="Enabled">
            <NSwitch
              v-model:value="providerForm.enabled"
              aria-label="Provider enabled"
            />
          </NFormItem>
        </NForm>
        <NText depth="3" class="small">
          The credential is sent once over the API and sealed server-side; it is
          never displayed, logged or stored in the browser again.
        </NText>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="providerOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="providerSaving"
            @click="handleSaveProvider"
          >
            Save
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NModal
      v-model:show="certificateOpen"
      preset="card"
      :title="editingCertificate ? 'Edit certificate configuration' : 'Add certificate'"
      style="width: 560px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NAlert
          v-if="certificateError"
          type="error"
          :show-icon="true"
        >
          {{ certificateError }}
        </NAlert>
        <CertificateForm
          v-model="certificateDraft"
          :applications="proxyStore.applications"
          :providers="proxyStore.providers"
          :lock-application="editingCertificate !== null"
        />
        <NText depth="3" class="small">
          The recorded domain always comes from the selected application's base
          domain — it is not editable here.
        </NText>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="certificateOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="certificateSaving"
            @click="handleSaveCertificate"
          >
            Save
          </NButton>
        </NSpace>
      </template>
    </NModal>
    <NModal
      v-model:show="redirectEditOpen"
      preset="card"
      title="Edit redirect rule"
      style="width: 560px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NAlert v-if="redirectEditError" type="error" :show-icon="true">
          {{ redirectEditError }}
        </NAlert>
        <NText depth="3" class="small">
          Application:
          <span class="mono">
            {{ applicationName(redirectEditForm.application_id) }}
          </span>
          — the owning application cannot be moved after creation.
        </NText>
        <NForm label-placement="top" :show-feedback="false">
          <NFormItem label="Source domain">
            <NInput
              v-model:value="redirectEditForm.source_domain"
              aria-label="Edit redirect source domain"
            />
          </NFormItem>
          <NFormItem label="Target domain">
            <NInput
              v-model:value="redirectEditForm.target_domain"
              aria-label="Edit redirect target domain"
            />
          </NFormItem>
          <NFormItem label="Redirect code">
            <NSelect
              v-model:value="redirectEditForm.code"
              :options="redirectCodeOptions"
              aria-label="Edit redirect code"
            />
          </NFormItem>
          <NFormItem label="Preserve path">
            <NSwitch
              v-model:value="redirectEditForm.preserve_path"
              aria-label="Edit redirect preserve path"
            />
          </NFormItem>
          <NFormItem label="Enabled">
            <NSwitch
              v-model:value="redirectEditForm.enabled"
              aria-label="Edit redirect enabled"
            />
          </NFormItem>
        </NForm>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="redirectEditOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="redirectEditSaving"
            @click="handleSaveRedirect"
          >
            Save
          </NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.domains-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.page-head h1 {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.mono {
  font-family: var(--font-mono);
}

.small {
  font-size: var(--text-xs);
}

.grid {
  display: grid;
  gap: var(--space-4);
}

.cols-4 {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.cols-2 {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.tabs {
  margin-top: var(--space-2);
}

.stat {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-4);
}

.stat-label {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--muted);
}

.stat-value {
  font-family: var(--font-display);
  font-size: var(--text-3xl);
  font-weight: 700;
  line-height: 1.1;
  letter-spacing: -0.02em;
  color: var(--fg-2);
  margin-top: 6px;
}

.stat-sub {
  font-size: var(--text-xs);
  color: var(--muted);
  margin-top: 6px;
}

.embed {
  border-left: 4px solid var(--accent);
  background: var(--surface);
  border-radius: var(--radius-sm);
  padding: var(--space-2) var(--space-3);
}

.embed--warn {
  border-left-color: var(--warn);
}

.embed h4 {
  font-size: var(--text-sm);
}

.embed p {
  font-size: var(--text-xs);
  color: var(--muted);
  margin-top: var(--space-1);
}

.channel-card {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
}

.channel-mark {
  width: 36px;
  height: 36px;
  border-radius: var(--radius-md);
  display: grid;
  place-items: center;
  background: var(--surface-warm);
  border: 1px solid var(--border);
  color: var(--fg-2);
  flex: 0 0 auto;
  font-size: var(--text-xs);
}

.channel-body {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.channel-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.channel-head h4 {
  font-size: var(--text-base);
}

.channel-actions {
  flex-wrap: wrap;
}

.kv {
  display: grid;
  grid-template-columns: minmax(110px, 140px) minmax(0, 1fr);
  gap: var(--space-2) var(--space-4);
  align-items: baseline;
  margin: 0;
}

.kv dt {
  font-size: var(--text-xs);
  color: var(--muted);
}

.kv dd {
  margin: 0;
  font-size: var(--text-xs);
}

.cell-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.cell-name {
  color: var(--fg-2);
  font-weight: 600;
}

.cell-sub {
  font-size: var(--text-xs);
  color: var(--muted);
}

.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
  max-width: 60ch;
}

.card-tag {
  font-size: 11px;
  color: var(--muted);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 2px 8px;
  white-space: nowrap;
}

.redirect-rows {
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
}

.domain-row {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr) minmax(0, 1fr) auto;
  gap: var(--space-3);
  align-items: center;
  padding: 10px var(--space-3);
  border-bottom: 1px solid var(--border);
  font-size: var(--text-xs);
}

.domain-row:last-child {
  border-bottom: 0;
}

.redirect-route {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  flex-wrap: wrap;
}

.redirect-arrow {
  color: var(--muted);
}

.redirect-code,
.redirect-state {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.redirect-actions {
  flex-wrap: wrap;
}

.state-dot {
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
  flex: 0 0 auto;
}

.state-dot--on {
  background: var(--success);
}

.state-dot--off {
  background: var(--muted);
}

.redirect-form__fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(172px, 1fr));
  gap: var(--form-item-gap);
  align-items: start;
}

.redirect-form__fields .n-form-item {
  margin: 0;
  min-width: 0;
}

/* Preserve path + Enabled share one row with the submit button, so the
 * inline form ends in a single action row instead of two lone switches. */
.redirect-form__bottom {
  display: flex;
  align-items: flex-end;
  gap: var(--space-4);
  flex-wrap: wrap;
  margin-top: var(--form-item-gap);
}

.redirect-form__bottom .n-form-item {
  margin: 0;
  min-width: 0;
}

@container (max-width: 480px) {
  .redirect-form__bottom {
    flex-direction: column;
    align-items: stretch;
  }
}

@media (max-width: 1024px) {
  .cols-4 {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 860px) {
  .cols-2 {
    grid-template-columns: minmax(0, 1fr);
  }

  .domain-row {
    grid-template-columns: minmax(0, 1fr);
  }

  .page-actions {
    margin-left: 0;
    width: 100%;
  }
}

@media (max-width: 640px) {
  .cols-4 {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
