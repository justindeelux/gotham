<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NInput,
  NModal,
  NPopconfirm,
  NSpace,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { RouterLink } from "vue-router";

import { describeApplicationError, updateApplication } from "../api/applications";
import type { Application } from "../api/applications";
import {
  certificateStatusLabel,
  certificateStatusTagType,
  describeProxyError,
  draftFromCertificate,
  providerLabel,
  toCertificateInput,
} from "../api/proxy";
import type { CertificateDraft, Certificate, DNSProvider } from "../api/proxy";
import CertificateForm from "./CertificateForm.vue";
import { useApplicationsStore } from "../stores/applications";
import { useProxyStore } from "../stores/proxy";
import { expiryLabel, formatDate, relativeTime } from "../utils/format";

/**
 * Domain and certificate editor of one application (the Domains tab of the
 * application detail page). The base domain is written through the
 * applications API; the certificate configuration through the proxy SSL API.
 * The certificate's recorded domain always comes from the application's
 * current base_domain, so a domain change is flagged until the configuration
 * is saved again.
 */

interface Props {
  application: Application;
}

const props = defineProps<Props>();

const message = useMessage();
const appsStore = useApplicationsStore();
const proxyStore = useProxyStore();

/** HOST_PATTERN mirrors the generator's ValidateDomain boundary. */
const HOST_PATTERN =
  /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$/;

const baseDomain = ref<string>(props.application.base_domain);
const savingDomain = ref(false);
const domainError = ref<string | null>(null);

const certificateOpen = ref(false);
const certificateSaving = ref(false);
const certificateError = ref<string | null>(null);
const certificateDraft = ref<CertificateDraft>({
  application_id: props.application.id,
  challenge: "http-01",
  dns_provider_id: "",
  wildcard: false,
  enabled: true,
});

const certificate = computed<Certificate | null>(() =>
  proxyStore.certificateOf(props.application.id),
);

/** recordedDomainDiffers flags a base domain changed after the last save. */
const recordedDomainDiffers = computed<boolean>(
  () =>
    certificate.value !== null &&
    certificate.value.domain !== props.application.base_domain,
);

/** providerName resolves a stored provider id to its display label. */
function providerName(providerId: string): string {
  if (!providerId) {
    return "shared HTTP resolver";
  }
  const provider: DNSProvider | null = proxyStore.providerOf(providerId);
  if (!provider) {
    return "unknown provider";
  }
  return `${provider.name || providerLabel(provider.provider)} · ${provider.provider}`;
}

/** isValidDomain accepts an empty value (clears) or a plain hostname. */
function isValidDomain(value: string): boolean {
  if (value === "") {
    return true;
  }
  return value.length <= 253 && HOST_PATTERN.test(value);
}

/** handleSaveDomain writes base_domain through the applications API. */
async function handleSaveDomain(): Promise<void> {
  const next = baseDomain.value.trim().toLowerCase();
  if (!isValidDomain(next)) {
    domainError.value =
      "Enter a plain hostname such as app.example.com (letters, digits, hyphens and dots; no wildcard).";
    return;
  }
  domainError.value = null;
  savingDomain.value = true;
  try {
    await updateApplication(props.application.id, { base_domain: next });
    baseDomain.value = next;
    await appsStore.fetchApplication(props.application.id);
    message.success("Application domain saved.");
  } catch (error) {
    domainError.value = describeApplicationError(error);
  } finally {
    savingDomain.value = false;
  }
}

/** openCertificateEdit seeds the dialog from the stored configuration. */
function openCertificateEdit(): void {
  certificateError.value = null;
  const existing = certificate.value;
  certificateDraft.value = existing
    ? draftFromCertificate(existing)
    : {
        application_id: props.application.id,
        challenge: "http-01",
        dns_provider_id: "",
        wildcard: false,
        enabled: true,
      };
  certificateOpen.value = true;
}

/** handleSaveCertificate creates or updates the configuration. */
async function handleSaveCertificate(): Promise<void> {
  certificateError.value = null;
  certificateSaving.value = true;
  try {
    const input = toCertificateInput(certificateDraft.value);
    const existing = certificate.value;
    if (existing) {
      await proxyStore.updateCertificateConfig(existing.id, input);
      message.success("Certificate configuration saved.");
    } else {
      await proxyStore.createCertificateConfig({
        ...input,
        application_id: props.application.id,
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

/** handleRerecordDomain re-saves the configuration to record the new domain. */
async function handleRerecordDomain(): Promise<void> {
  const existing = certificate.value;
  if (!existing) {
    return;
  }
  certificateError.value = null;
  try {
    await proxyStore.updateCertificateConfig(existing.id, {});
    message.success("Certificate re-recorded the current application domain.");
  } catch (error) {
    message.error(describeProxyError(error));
  }
}

/** handleDeleteCertificate removes the configuration. */
async function handleDeleteCertificate(): Promise<void> {
  const existing = certificate.value;
  if (!existing) {
    return;
  }
  try {
    await proxyStore.removeCertificate(existing.id);
    message.success("Certificate configuration deleted. The route stays HTTP-only.");
  } catch (error) {
    message.error(describeProxyError(error));
  }
}

/** load refreshes the providers and certificates the editor depends on. */
async function load(): Promise<void> {
  await Promise.allSettled([
    proxyStore.fetchProviders(),
    proxyStore.fetchCertificates(),
  ]);
}

watch(
  () => props.application.base_domain,
  (value) => {
    baseDomain.value = value;
  },
);

watch(
  () => props.application.id,
  () => {
    certificateError.value = null;
    void load();
  },
);

onMounted(() => {
  void load();
});
</script>

<template>
  <NSpace vertical :size="16">
    <NCard title="Application domain">
      <NSpace vertical :size="12">
        <NText depth="3">
          The control plane routes this hostname to the application container.
          The certificate configuration records it at save time.
        </NText>
        <NAlert
          v-if="domainError"
          type="error"
          :show-icon="true"
        >
          {{ domainError }}
        </NAlert>
        <NSpace align="center" :size="8" :wrap="false">
          <NInput
            v-model:value="baseDomain"
            class="mono"
            style="max-width: 360px"
            placeholder="app.example.com"
            aria-label="Application base domain"
            @keyup.enter="handleSaveDomain"
          />
          <NButton
            type="primary"
            :loading="savingDomain"
            @click="handleSaveDomain"
          >
            Save domain
          </NButton>
        </NSpace>
        <NText depth="3" class="small">
          Leave empty to remove the domain. HTTP and HTTPS routing only exist
          while the application has a valid domain.
        </NText>
      </NSpace>
    </NCard>

    <NCard title="Certificate configuration">
      <template #header-extra>
        <NButton
          v-if="certificate"
          size="small"
          @click="openCertificateEdit"
        >
          Edit
        </NButton>
      </template>
      <NSpace vertical :size="12">
        <NAlert
          v-if="proxyStore.certificatesError"
          type="error"
          :show-icon="true"
        >
          {{ proxyStore.certificatesError }}
        </NAlert>
        <NAlert
          v-if="recordedDomainDiffers && certificate"
          type="warning"
          :show-icon="true"
        >
          This configuration still records
          <span class="mono">{{ certificate.domain }}</span>. Saving it again
          records the current base domain
          <span class="mono">{{ application.base_domain }}</span>.
          <NSpace style="margin-top: 8px">
            <NButton size="small" @click="handleRerecordDomain">
              Re-record domain
            </NButton>
          </NSpace>
        </NAlert>

        <template v-if="certificate">
          <NDescriptions
            :column="1"
            bordered
            label-placement="left"
            size="small"
          >
            <NDescriptionsItem label="Recorded domain">
              <span class="mono">{{ certificate.domain }}</span>
            </NDescriptionsItem>
            <NDescriptionsItem label="Challenge">
              <span class="mono">{{ certificate.challenge }}</span>
            </NDescriptionsItem>
            <NDescriptionsItem label="DNS provider">
              <span class="mono">{{ providerName(certificate.dns_provider_id) }}</span>
            </NDescriptionsItem>
            <NDescriptionsItem label="Wildcard">
              <NTag v-if="certificate.wildcard" size="small">requested</NTag>
              <NText v-else depth="3">no</NText>
            </NDescriptionsItem>
            <NDescriptionsItem label="Enabled">
              <NTag :type="certificate.enabled ? 'success' : 'default'" size="small">
                {{ certificate.enabled ? "enabled" : "disabled" }}
              </NTag>
            </NDescriptionsItem>
            <NDescriptionsItem label="Status">
              <NTag size="small" :type="certificateStatusTagType(certificate.status)">
                {{ certificateStatusLabel(certificate.status) }}
              </NTag>
            </NDescriptionsItem>
            <NDescriptionsItem label="Expires">
              <template v-if="certificate.status === 'present' && certificate.not_after">
                <span class="mono">{{ formatDate(certificate.not_after) }}</span>
                <span class="small hint"> · {{ expiryLabel(certificate.not_after) }}</span>
              </template>
              <NText v-else-if="certificate.status === 'present'" depth="3">
                not reported
              </NText>
              <NText v-else depth="3">—</NText>
            </NDescriptionsItem>
            <NDescriptionsItem label="Updated">
              {{ relativeTime(certificate.updated_at) }}
            </NDescriptionsItem>
          </NDescriptions>
          <NSpace :size="8">
            <NPopconfirm
              :positive-button-props="{ type: 'error' }"
              @positive-click="handleDeleteCertificate"
            >
              <template #trigger>
                <NButton size="small" type="error" ghost>
                  Delete configuration
                </NButton>
              </template>
              Delete the certificate configuration for
              {{ application.base_domain || "this application" }}? The route
              falls back to plain HTTP.
            </NPopconfirm>
          </NSpace>
          <NText depth="3" class="small">
            Status and expiry are observed from the node's ACME storage on
            read. unknown means the node could not be read — never a
            fabricated status.
          </NText>
        </template>

        <NEmpty
          v-else
          description="No certificate configuration for this application yet."
        >
          <template #extra>
            <NButton type="primary" @click="openCertificateEdit">
              Configure certificate
            </NButton>
          </template>
        </NEmpty>
      </NSpace>
      <template #footer>
        <NText depth="3" class="small">
          One configuration per application.
          <RouterLink :to="{ name: 'domains' }">
            Manage all certificates and DNS providers
          </RouterLink>
        </NText>
      </template>
    </NCard>

    <NModal
      v-model:show="certificateOpen"
      preset="card"
      :title="certificate ? 'Edit certificate configuration' : 'Configure certificate'"
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
          :applications="[application]"
          :providers="proxyStore.providers"
          lock-application
        />
        <NText depth="3" class="small">
          The domain is not edited here — it comes from the application's base
          domain above.
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
  </NSpace>
</template>

<style scoped>
.small {
  font-size: var(--text-xs);
}

.hint {
  color: var(--muted);
}
</style>
