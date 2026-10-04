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
