<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NSpace,
  NSpin,
  NText,
} from "naive-ui";
import { inject } from "vue";

import { databaseDetailKey } from "@/features/databases/composables/useDatabaseDetail";
import { relativeTime } from "@/shared/utils/format";

const detail = inject(databaseDetailKey)!;
</script>

<template>
  <NSpace vertical :size="16" style="margin-top: 16px">
    <NCard v-if="detail.database.value" title="Details">
      <NDescriptions :column="detail.descColumns.value" bordered label-placement="left">
        <NDescriptionsItem label="Engine">
          <span class="mono">{{ detail.engineLabel.value }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Node">
          <span class="mono">{{ detail.serverLabel.value }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Public port">
          <span class="mono">
            {{
              (detail.database.value?.public_port ?? 0) > 0
                ? detail.database.value?.public_port
                : "off · internal network only"
            }}
          </span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Volume">
          <span class="mono">{{ detail.database.value?.volume }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Container">
          <span class="mono">{{ detail.database.value?.container_id || "—" }}</span>
        </NDescriptionsItem>
        <NDescriptionsItem label="Created">
          {{ relativeTime(detail.database.value?.created_at ?? "") }}
        </NDescriptionsItem>
      </NDescriptions>
    </NCard>

    <NCard title="Credentials">
      <template #header-extra>
        <NText depth="3">Stored encrypted · owner only</NText>
      </template>
      <NAlert
        v-if="detail.databasesStore.credentialsError"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ detail.databasesStore.credentialsError }}
      </NAlert>
      <NSpin :show="detail.databasesStore.credentialsLoading">
        <NSpace v-if="detail.credentials.value" vertical :size="12">
          <div class="credential-row">
            <NText depth="3">Username</NText>
            <NText class="mono grow">{{ detail.credentials.value.username }}</NText>
            <NButton
              size="small"
              secondary
              @click="() => detail.copyCredential('username', 'Username')"
            >
              Copy
            </NButton>
          </div>
          <div class="credential-row">
            <NText depth="3">Password</NText>
            <NText class="mono grow">
              {{ detail.revealed.value ? detail.credentials.value.password : "••••••••••••" }}
            </NText>
            <NButton
              size="small"
              secondary
              @click="detail.revealed.value = !detail.revealed.value"
            >
              {{ detail.revealed.value ? "Hide" : "Reveal" }}
            </NButton>
            <NButton
              size="small"
              secondary
              @click="() => detail.copyCredential('password', 'Password')"
            >
              Copy
            </NButton>
          </div>
          <div class="credential-row">
            <NText depth="3">Database</NText>
            <NText class="mono grow">{{ detail.credentials.value.database }}</NText>
            <NButton
              size="small"
              secondary
              @click="() => detail.copyCredential('database', 'Database')"
            >
              Copy
            </NButton>
          </div>
          <div v-if="detail.credentials.value.root_password" class="credential-row">
            <NText depth="3">Root password</NText>
            <NText class="mono grow">
              {{ detail.revealed.value ? detail.credentials.value.root_password : "••••••••••••" }}
            </NText>
            <NButton
              size="small"
              secondary
              @click="() => detail.copyCredential('root_password', 'Root password')"
            >
              Copy
            </NButton>
          </div>
          <div v-if="detail.connectionString.value" class="connection-block">
            <NText depth="3" class="connection-label">
              Connection string
            </NText>
            <pre class="connection-string"><code>{{ detail.connectionDisplay.value }}</code></pre>
            <NButton size="small" secondary @click="detail.copyConnectionString">
              Copy connection string
            </NButton>
          </div>
          <NText v-else-if="detail.nodeAddressUnknown.value" depth="3">
            Public endpoint unavailable · node address unknown.
          </NText>
        </NSpace>
        <NEmpty
          v-else-if="!detail.databasesStore.credentialsLoading"
          description="No credentials cached — they load automatically with the page."
        />
      </NSpin>
    </NCard>
  </NSpace>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.grow {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.credential-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.connection-block {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--space-2);
}

.connection-label {
  font-size: var(--text-sm);
}

.connection-string {
  margin: 0;
  width: 100%;
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-3);
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

.connection-string code {
  font-family: inherit;
}
</style>
