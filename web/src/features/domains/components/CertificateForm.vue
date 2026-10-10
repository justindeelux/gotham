<script setup lang="ts">
import {
  NForm,
  NFormItem,
  NRadio,
  NRadioGroup,
  NSelect,
  NSwitch,
  NTag,
  NText,
} from "naive-ui";
import { computed } from "vue";

import type { Application } from "@/features/applications";
import { providerLabel, proxyText } from "@/features/domains/api/proxy";
import type { CertificateDraft, DNSProvider } from "@/features/domains/api/proxy";

/**
 * Certificate configuration editor used by the Domains page modal and the
 * application detail Domain editor. The recorded domain is never edited here:
 * it always comes from the selected application's base_domain.
 */

interface Props {
  modelValue: CertificateDraft;
  applications: Application[];
  providers: DNSProvider[];
  /** When true the application is fixed (editing a stored configuration). */
  lockApplication?: boolean;
}

const props = withDefaults(defineProps<Props>(), { lockApplication: false });
const emit = defineEmits<{
  "update:modelValue": [value: CertificateDraft];
}>();

const applicationOptions = computed(() =>
  props.applications.map((application) => ({
    label: application.base_domain
      ? `${application.name} · ${application.base_domain}`
      : proxyText(
          "domains.certificateForm.appWithoutDomain",
          "{name} · no base domain",
          { name: application.name },
        ),
    value: application.id,
  })),
);

const providerOptions = computed(() =>
  props.providers.map((provider) => ({
    label: provider.enabled
      ? `${provider.name || providerLabel(provider.provider)} · ${provider.provider}`
      : proxyText(
          "domains.certificateForm.providerDisabled",
          "{label} · {provider} (disabled)",
          {
            label: provider.name || providerLabel(provider.provider),
            provider: provider.provider,
          },
        ),
    value: provider.id,
  })),
);

const selectedApplication = computed<Application | null>(
  () =>
    props.applications.find(
      (application) => application.id === props.modelValue.application_id,
    ) ?? null,
);

/** domainPreview shows the exact host the API will record. */
const domainPreview = computed<string>(
  () => selectedApplication.value?.base_domain ?? "",
);

/** isDns01 drives the provider and wildcard controls. */
const isDns01 = computed<boolean>(() => props.modelValue.challenge === "dns-01");

/** patch emits one updated draft, keeping the object immutable. */
function patch(changes: Partial<CertificateDraft>): void {
  emit("update:modelValue", { ...props.modelValue, ...changes });
}

/** setChallenge resets dns-only fields when switching back to http-01. */
function setChallenge(challenge: string): void {
  const next = challenge === "dns-01" ? "dns-01" : "http-01";
  if (next === "http-01") {
    patch({ challenge: next, dns_provider_id: "", wildcard: false });
    return;
  }
  patch({ challenge: next });
}
</script>

<template>
  <NForm label-placement="top" :show-feedback="false" class="certificate-form form-container">
    <NFormItem :label="$t('domains.certificateForm.application')" class="field-application">
      <NSelect
        :value="modelValue.application_id"
        :options="applicationOptions"
        :disabled="lockApplication"
        :placeholder="$t('domains.certificateForm.selectApplication')"
        :aria-label="$t('domains.certificateForm.applicationAria')"
        @update:value="(value: string) => patch({ application_id: value })"
      />
    </NFormItem>
    <NFormItem :label="$t('domains.certificateForm.domainFromApp')">
      <NText v-if="domainPreview" class="mono">{{ domainPreview }}</NText>
      <NText v-else depth="3">
        {{ $t("domains.certificateForm.noBaseDomain") }}
      </NText>
    </NFormItem>
    <NFormItem :label="$t('domains.certificateForm.challenge')">
      <NRadioGroup
        :value="modelValue.challenge"
        @update:value="(value: string | number) => setChallenge(String(value))"
      >
        <NRadio value="http-01">
          <span class="mono">http-01</span> {{ $t("domains.certificateForm.httpHint") }}
        </NRadio>
        <NRadio value="dns-01">
          <span class="mono">dns-01</span> {{ $t("domains.certificateForm.dnsHint") }}
        </NRadio>
      </NRadioGroup>
    </NFormItem>
    <NFormItem :label="$t('domains.certificateForm.dnsProvider')" class="field-provider-select">
      <NSelect
        :value="modelValue.dns_provider_id"
        :options="providerOptions"
        :disabled="!isDns01"
        :placeholder="$t('domains.certificateForm.selectProvider')"
        :aria-label="$t('domains.certificateForm.dnsProviderAria')"
        clearable
        @update:value="(value: string | number | null) =>
          patch({ dns_provider_id: value === null ? '' : String(value) })
        "
      />
      <NText v-if="!isDns01" depth="3" class="hint">
        {{ $t("domains.certificateForm.httpNoProvider") }}
      </NText>
    </NFormItem>
    <NFormItem :label="$t('domains.certificateForm.wildcard')">
      <NSwitch
        :value="modelValue.wildcard"
        :disabled="!isDns01"
        :aria-label="$t('domains.certificateForm.wildcardAria')"
        @update:value="(value: boolean) => patch({ wildcard: value })"
      />
      <NTag v-if="modelValue.wildcard" size="small" class="hint">{{ $t("domains.certificateForm.wildcardRequested") }}</NTag>
      <NText v-else depth="3" class="hint">
        {{ $t("domains.certificateForm.wildcardRequires") }}
      </NText>
    </NFormItem>
    <NFormItem :label="$t('domains.certificateForm.enabled')">
      <NSwitch
        :value="modelValue.enabled"
        :aria-label="$t('domains.certificateForm.enabledAria')"
        @update:value="(value: boolean) => patch({ enabled: value })"
      />
    </NFormItem>
  </NForm>
</template>

<style scoped>
.hint {
  margin-left: var(--space-2);
  font-size: var(--text-xs);
}
</style>
