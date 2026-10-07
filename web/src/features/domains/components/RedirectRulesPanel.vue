<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NPopconfirm,
  NSpace,
  NSwitch,
  NTag,
  NText,
} from "naive-ui";

import { useRedirects } from "@/features/domains/composables/useRedirects";
import { useProxyStore } from "@/features/domains/stores/proxy";

const proxyStore = useProxyStore();
const {
  enabledRedirects,
  openRedirectEdit,
  handleToggleRedirect,
  handleDeleteRedirect,
} = useRedirects();
</script>

<template>
  <NCard style="margin-top: 16px" :title="$t('domains.redirects.rulesTitle')">
    <template #header-extra>
      <span class="card-tag mono">{{ $t("domains.redirects.middlewareTag") }}</span>
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
              {{ redirect.code === 301 ? $t("domains.redirects.codePermanent") : $t("domains.redirects.codeTemporary") }}
            </span>
          </div>
          <div class="redirect-state">
            <span
              class="state-dot"
              :class="redirect.enabled ? 'state-dot--on' : 'state-dot--off'"
              aria-hidden="true"
            ></span>
            <span class="cell-sub">
              {{ redirect.enabled ? $t("domains.providers.enabled") : $t("domains.redirects.paused") }}
            </span>
            <span v-if="redirect.preserve_path" class="cell-sub">
              {{ $t("domains.redirects.keepsPath") }}
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
              :aria-label="$t('domains.redirects.enableRedirectAria', { source: redirect.source_domain })"
              @update:value="(value: boolean) => handleToggleRedirect(redirect, value)"
            />
            <NButton size="small" @click="openRedirectEdit(redirect)">
              {{ $t("domains.redirects.edit") }}
            </NButton>
            <NPopconfirm
              :positive-button-props="{ type: 'error' }"
              @positive-click="handleDeleteRedirect(redirect)"
            >
              <template #trigger>
                <NButton size="small" type="error" ghost>{{ $t("domains.redirects.delete") }}</NButton>
              </template>
              {{ $t("domains.redirects.deleteConfirm", { source: redirect.source_domain, target: redirect.target_domain }) }}
            </NPopconfirm>
          </NSpace>
        </div>
      </div>
      <NEmpty
        v-else-if="!proxyStore.redirectsLoading"
        :description="$t('domains.redirects.empty')"
      >
        <template #extra>
          <p class="empty-hint">
            {{ $t("domains.redirects.emptyHint") }}
          </p>
        </template>
      </NEmpty>
      <p v-if="proxyStore.redirects.length > 0" class="cell-sub">
        {{
          $t(
            "domains.redirects.rulesSummary",
            {
              total: proxyStore.redirects.length,
              enabled: enabledRedirects,
            },
            { plural: proxyStore.redirects.length },
          )
        }}
      </p>
    </NSpace>
    <template #footer>
      <NText depth="3" class="small">
        {{ $t("domains.redirects.footer") }}
      </NText>
    </template>
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.small {
  font-size: var(--text-xs);
}

.card-tag {
  font-size: 11px;
  color: var(--muted);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: 2px 8px;
  white-space: nowrap;
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

@media (max-width: 860px) {
  .domain-row {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
