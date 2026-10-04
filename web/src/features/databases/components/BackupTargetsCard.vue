<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NInput,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import { inject } from "vue";

import {
  TARGET_KIND_OPTIONS,
  databaseBackupsKey,
} from "@/features/databases/composables/useDatabaseBackups";

const backups = inject(databaseBackupsKey)!;
</script>

<template>
  <NCard title="Backup targets">
    <template #header-extra>
      <NText depth="3">S3-compatible storage</NText>
    </template>
    <NAlert
      v-if="backups.backupsStore.targetsError"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ backups.backupsStore.targetsError }}
    </NAlert>
    <NSpin :show="backups.backupsStore.targetsLoading">
      <NSpace
        v-if="backups.backupsStore.targets.length > 0"
        vertical
        :size="12"
        style="width: 100%"
      >
        <div
          v-for="target in backups.backupsStore.targets"
          :key="target.id"
          class="backup-row"
        >
          <div class="backup-row__main">
            <NSpace align="center" :size="8">
              <NText strong>{{ target.name }}</NText>
              <NTag size="small" :bordered="false">
                {{ target.kind }}
              </NTag>
              <NTag
                v-if="target.has_credentials"
                size="small"
                type="success"
              >
                Credentials configured
              </NTag>
              <NTag v-else size="small" type="warning">
                No credentials
              </NTag>
            </NSpace>
            <NText class="mono backup-row__location" depth="3">
              {{
                target.kind === "s3"
                  ? `${target.endpoint ?? ""} · ${target.bucket ?? ""}${target.prefix ? ` · ${target.prefix}` : ""}`
                  : "control plane disk"
              }}
            </NText>
            <NText
              v-if="
                backups.targetTests.value[target.id] &&
                !backups.targetTests.value[target.id].checking
              "
              :type="
                backups.targetTests.value[target.id].ok ? 'success' : 'error'
              "
            >
              {{ backups.targetTests.value[target.id].message }}
            </NText>
          </div>
          <NSpace class="backup-row__actions" align="center" :size="8">
            <NButton
              size="small"
              secondary
              :aria-label="`Test target ${target.name}`"
              :loading="backups.targetTests.value[target.id]?.checking"
              @click="() => void backups.handleTestTarget(target.id)"
            >
              Test
            </NButton>
            <NButton
              size="small"
              secondary
              :aria-label="`Edit target ${target.name}`"
              @click="backups.openTargetEdit(target.id)"
            >
              Edit
            </NButton>
            <NPopconfirm
              @positive-click="
                () => void backups.handleDeleteTarget(target.id, target.name)
              "
            >
              <template #trigger>
                <NButton
                  size="small"
                  type="error"
                  ghost
                  :aria-label="`Delete target ${target.name}`"
                >
                  Delete
                </NButton>
              </template>
              Delete target "{{ target.name }}"? Past backups keep
              their location but can no longer be read back from
              this target.
            </NPopconfirm>
          </NSpace>
        </div>
      </NSpace>
      <NEmpty
        v-else-if="!backups.backupsStore.targetsLoading"
        description="No backup targets yet"
      >
        <template #extra>
          <p class="empty-hint">
            Backups fall back to the control plane disk until an
            S3-compatible target is configured.
          </p>
        </template>
      </NEmpty>
    </NSpin>
    <div class="target-form">
      <NText strong>
        {{ backups.targetEditingId.value === null ? "New target" : "Edit target" }}
      </NText>
      <div class="target-form__grid">
        <NInput
          v-model:value="backups.targetName.value"
          placeholder="Target name"
          aria-label="Target name"
        />
        <NSelect
          v-model:value="backups.targetKind.value"
          :options="TARGET_KIND_OPTIONS"
          aria-label="Target kind"
        />
      </div>
      <template v-if="backups.targetKind.value === 's3'">
        <div class="target-form__grid">
          <NInput
            v-model:value="backups.targetEndpoint.value"
            class="mono"
            placeholder="https://…endpoint"
            aria-label="Endpoint"
          />
          <NInput
            v-model:value="backups.targetRegion.value"
            class="mono"
            placeholder="Region (e.g. auto)"
            aria-label="Region"
          />
        </div>
        <div class="target-form__grid">
          <NInput
            v-model:value="backups.targetBucket.value"
            class="mono"
            placeholder="Bucket"
            aria-label="Bucket"
          />
          <NInput
            v-model:value="backups.targetPrefix.value"
            class="mono"
            placeholder="Key prefix (optional)"
            aria-label="Key prefix"
          />
        </div>
        <div class="target-form__grid">
          <NInput
            v-model:value="backups.targetAccessKey.value"
            type="password"
            class="mono"
            :placeholder="
              backups.targetEditingId.value === null
                ? 'Access key'
                : 'Access key · blank keeps stored keys'
            "
            aria-label="Access key"
          />
          <NInput
            v-model:value="backups.targetSecretKey.value"
            type="password"
            class="mono"
            :placeholder="
              backups.targetEditingId.value === null
                ? 'Secret key'
                : 'Secret key · blank keeps stored keys'
            "
            aria-label="Secret key"
          />
        </div>
        <NText depth="3">
          Secrets are sealed on the server and never shown back —
          leave the key fields blank to keep the stored ones.
        </NText>
      </template>
      <NSpace justify="end" :size="8">
        <NButton
          v-if="backups.targetEditingId.value !== null"
          @click="backups.resetTargetForm()"
        >
          Cancel
        </NButton>
        <NButton
          type="primary"
          :loading="backups.backupsStore.targetsActing"
          @click="() => void backups.handleSaveTarget()"
        >
          {{
            backups.targetEditingId.value === null ? "Add target" : "Save target"
          }}
        </NButton>
      </NSpace>
    </div>
  </NCard>
</template>

<style scoped src="./backupRows.css">
</style>

<style scoped>
.target-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  margin-top: var(--space-4);
  padding-top: var(--space-4);
  border-top: 1px dashed var(--border);
}

.target-form__grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-2);
}

@media (max-width: 640px) {
  .target-form__grid {
    grid-template-columns: 1fr;
  }
}
</style>
