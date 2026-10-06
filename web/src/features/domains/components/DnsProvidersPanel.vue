<script setup lang="ts">
import {
  NButton,
  NCard,
  NEmpty,
  NPopconfirm,
  NSpace,
  NSwitch,
  NTag,
  NText,
} from "naive-ui";

import { providerLabel } from "@/features/domains/api/proxy";
import { useProviders } from "@/features/domains/composables/useProviders";
import { useProxyStore } from "@/features/domains/stores/proxy";
import { relativeTime } from "@/shared/utils/format";

const proxyStore = useProxyStore();
const { openProviderCreate, openProviderEdit, handleToggleProvider, handleDeleteProvider } =
  useProviders();
</script>

<template>
  <NCard style="margin-top: 16px" :title="$t('domains.providers.title')">
    <template #header-extra>
      <NButton size="small" @click="openProviderCreate">
        {{ $t("domains.providers.add") }}
      </NButton>
    </template>
    <NSpace vertical :size="12">
      <NText depth="3">
        {{ $t("domains.providers.sealedNote") }}
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
                {{ provider.enabled ? $t("domains.providers.enabled") : $t("domains.providers.disabled") }}
              </NTag>
            </div>
            <dl class="kv">
              <dt>{{ $t("domains.providers.zones") }}</dt>
              <dd class="mono">{{ provider.zones.join(", ") }}</dd>
              <dt>{{ $t("domains.providers.credential") }}</dt>
              <dd class="mono">
                {{ provider.credentials_set ? $t("domains.providers.credentialSet") : $t("domains.providers.credentialUnset") }} {{ $t("domains.providers.credentialNeverReturned") }}
              </dd>
              <dt>{{ $t("domains.providers.updated") }}</dt>
              <dd>{{ relativeTime(provider.updated_at) }}</dd>
            </dl>
            <NSpace :size="8" align="center" class="channel-actions">
              <NSwitch
                :value="provider.enabled"
                :aria-label="$t('domains.providers.enableLabel', { name: provider.name || provider.provider })"
                @update:value="(value: boolean) => handleToggleProvider(provider, value)"
              />
              <NButton size="small" @click="openProviderEdit(provider)">
                {{ $t("domains.providers.editRotate") }}
              </NButton>
              <NPopconfirm
                :positive-button-props="{ type: 'error' }"
                @positive-click="handleDeleteProvider(provider)"
              >
                <template #trigger>
                  <NButton size="small" type="error" ghost>
                    {{ $t("domains.providers.delete") }}
                  </NButton>
                </template>
                {{ $t("domains.providers.deleteConfirm", { provider: provider.provider, name: provider.name || "" }) }}
              </NPopconfirm>
            </NSpace>
          </div>
        </div>
      </div>
      <NEmpty
        v-else
        :description="$t('domains.providers.empty')"
      >
        <template #extra>
          <p class="empty-hint">
            {{ $t("domains.providers.emptyHint") }}
          </p>
          <NButton type="primary" @click="openProviderCreate">
            {{ $t("domains.providers.add") }}
          </NButton>
        </template>
      </NEmpty>
      <div class="embed embed--warn">
        <h4>{{ $t("domains.providers.dns01Title") }}</h4>
        <p>
          {{ $t("domains.providers.dns01BodyPre") }}
          <span class="mono">_acme-challenge.&lt;domain&gt;</span>
          {{ $t("domains.providers.dns01BodyPost") }}
        </p>
      </div>
    </NSpace>
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.grid {
  display: grid;
  gap: var(--space-4);
}

.cols-2 {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
  max-width: 60ch;
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

@media (max-width: 860px) {
  .cols-2 {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
