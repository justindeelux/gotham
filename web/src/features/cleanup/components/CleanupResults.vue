<script setup lang="ts">
import {
  NButton,
  NCard,
  NCheckbox,
  NCollapse,
  NCollapseItem,
  NEmpty,
  NTag,
} from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { ScanNodeState } from "@/features/cleanup/composables/useCleanupPage";
import {
  categoryGroups,
  type SelectableItem,
} from "@/features/cleanup/utils/selection";
import {
  noticeKey,
  reasonKey,
  tierTagType,
} from "@/features/cleanup/utils/cleanupView";
import { formatBytes, relativeTime } from "@/shared/utils/format";

interface Props {
  scans: ScanNodeState[];
  items: SelectableItem[];
  selected: Set<string>;
  serverName: (_serverId: string) => string;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  toggleItem: [key: string, checked: boolean];
  setCategory: [category: string, select: boolean];
}>();

const { t, te } = useI18n();

const groups = computed(() => categoryGroups(props.items));

function categoryLabel(category: string): string {
  const key = `cleanup.categories.${category}`;
  return te(key) ? t(key) : category;
}

function statusLabel(status: ScanNodeState["status"]): string {
  return t(`cleanup.scan.status.${status}`);
}

function reasonText(reason: string): string {
  const key = reasonKey(reason);
  if (key) {
    return t(key);
  }
  return reason || t("cleanup.reasons.unknown");
}

function noticeText(reason: string): string {
  const key = noticeKey(reason);
  return key ? t(key) : reason;
}

function ageText(createdAt: string): string {
  return createdAt ? relativeTime(createdAt) : "—";
}

function nodeTotals(scan: ScanNodeState): string {
  const bytes = scan.items.reduce((sum, item) => sum + item.size_bytes, 0);
  return t("cleanup.scan.found", {
    count: scan.items.length,
    size: formatBytes(bytes),
  });
}

function selectedCount(groupItems: SelectableItem[]): number {
  return groupItems.filter((item) => props.selected.has(item.key)).length;
}
</script>

<template>
  <NCard :title="t('cleanup.results.title')" class="cleanup-card">
    <div class="cleanup-scan-summary">
      <div v-for="scan in scans" :key="scan.serverId" class="cleanup-scan-summary__node">
        <span class="cleanup-scan-summary__name">{{ serverName(scan.serverId) }}</span>
        <NTag
          size="small"
          :type="scan.status === 'done' ? 'success' : scan.status === 'failed' ? 'error' : 'warning'"
        >
          {{ statusLabel(scan.status) }}
        </NTag>
        <span v-if="scan.status === 'done'" class="cleanup-scan-summary__meta">
          {{ nodeTotals(scan) }}
        </span>
        <span v-if="scan.reason" class="cleanup-scan-summary__meta">
          {{ reasonText(scan.reason) }}
        </span>
        <span
          v-for="(notice, index) in scan.notices"
          :key="`${scan.serverId}-notice-${index}`"
          class="cleanup-scan-summary__meta"
        >
          {{ categoryLabel(notice.category) }}: {{ noticeText(notice.reason) }}
        </span>
      </div>
    </div>

    <NEmpty v-if="items.length === 0" :description="t('cleanup.results.empty')" />

    <NCollapse
      v-else
      :default-expanded-names="groups.map((group) => group.category)"
      class="cleanup-groups"
    >
      <NCollapseItem
        v-for="group in groups"
        :key="group.category"
        :name="group.category"
      >
        <template #header>
          <span class="cleanup-group__title">{{ categoryLabel(group.category) }}</span>
          <NTag size="small" :type="tierTagType(group.tier)">
            {{ t(`cleanup.tiers.${group.tier}`) }}
          </NTag>
          <span class="cleanup-group__total">
            {{ t("cleanup.results.categoryTotal", {
              count: group.count,
              size: formatBytes(group.bytes),
            }) }}
          </span>
        </template>
        <template #header-extra>
          <span class="cleanup-group__actions">
            <NButton
              text
              size="small"
              :aria-label="`${t('cleanup.actions.selectAll')} ${categoryLabel(group.category)}`"
              @click.stop="emit('setCategory', group.category, true)"
            >
              {{ t("cleanup.actions.selectAll") }}
            </NButton>
            <NButton
              text
              size="small"
              :disabled="selectedCount(group.items) === 0"
              :aria-label="`${t('cleanup.actions.selectNone')} ${categoryLabel(group.category)}`"
              @click.stop="emit('setCategory', group.category, false)"
            >
              {{ t("cleanup.actions.selectNone") }}
            </NButton>
          </span>
        </template>

        <ul class="cleanup-items">
          <li v-for="item in group.items" :key="item.key" class="cleanup-item">
            <NCheckbox
              :checked="selected.has(item.key)"
              @update:checked="(checked: boolean) => emit('toggleItem', item.key, checked)"
            >
              <span class="cleanup-item__name">{{ item.name }}</span>
            </NCheckbox>
            <span class="cleanup-item__meta">{{ formatBytes(item.sizeBytes) }}</span>
            <span class="cleanup-item__meta">{{ ageText(item.createdAt) }}</span>
            <span class="cleanup-item__meta">{{ serverName(item.serverId) }}</span>
            <span class="cleanup-item__meta cleanup-item__reason">
              {{ reasonText(item.reason) }}
            </span>
          </li>
        </ul>
      </NCollapseItem>
    </NCollapse>
  </NCard>
</template>

<style scoped>
.cleanup-scan-summary {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 220px), 1fr));
  gap: var(--space-2) var(--space-4);
  margin-bottom: var(--space-4);
}

.cleanup-scan-summary__node {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.cleanup-scan-summary__name {
  font-weight: 600;
  color: var(--fg-2);
}

.cleanup-scan-summary__meta {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--muted);
}

.cleanup-groups :deep(.n-collapse-item__header-main) {
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.cleanup-group__title {
  font-weight: 600;
  color: var(--fg-2);
}

.cleanup-group__total {
  font-size: var(--text-xs);
  color: var(--muted);
}

.cleanup-group__actions {
  display: inline-flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}

.cleanup-items {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
}

.cleanup-item {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-3);
  min-width: 0;
  padding: var(--space-1) 0;
}

.cleanup-item__name {
  min-width: 0;
  overflow-wrap: anywhere;
  font-weight: 500;
  color: var(--fg);
}

.cleanup-item__meta {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--muted);
}

.cleanup-item__reason {
  flex: 1 1 180px;
}
</style>
