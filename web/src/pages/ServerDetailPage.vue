<script setup lang="ts">
import {
  NAlert,
  NAvatar,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NPopconfirm,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  NText,
  useMessage,
} from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";

import type { Server } from "../api/servers";
import { describeServerError, getServer } from "../api/servers";
import ServerStatusTag from "../components/ServerStatusTag.vue";
import { useServersStore } from "../stores/servers";
import { relativeTime } from "../utils/format";

const route = useRoute();
const router = useRouter();
const message = useMessage();
const serversStore = useServersStore();

const serverId = computed<string>(() => String(route.params.id ?? ""));

const server = ref<Server | null>(null);
const loading = ref(false);
const error = ref<string | null>(null);
const validating = ref(false);
const deleting = ref(false);
const activeTab = ref("overview");

/** initials derives a two-letter avatar from the server name. */
const initials = computed<string>(() => {
  const name = server.value?.name ?? "";
  const letters = name.replace(/[^A-Za-z0-9]/g, "");
  if (letters.length >= 2) {
    return letters.slice(0, 2).toUpperCase();
  }
  if (letters.length === 1) {
    return letters.toUpperCase();
  }
  return "ND";
});

/** summaryLine renders the one-line node summary under the title. */
const summaryLine = computed<string>(() => {
  if (!server.value) {
    return "";
  }
  const parts = [
    `${server.value.ip}:${server.value.port}`,
    server.value.os ?? "Unknown OS",
    server.value.arch ?? "Unknown arch",
    server.value.docker_version ?? "Docker unknown",
  ];
  return parts.join(" · ");
});

/** fallback renders a nullable string field as display text. */
function fallback(value: string | null): string {
  return value ?? "—";
}

/** usageText renders a nullable percentage as display text. */
function usageText(value: number | null): string {
  if (value === null || value === undefined) {
    return "—";
  }
  return `${Math.round(value)}%`;
}

/** fetchServer loads one server by route id; 404 surfaces as an error state. */
async function fetchServer(): Promise<void> {
  if (!serverId.value) {
    error.value = "Unknown server.";
    return;
  }
  loading.value = true;
  error.value = null;
  try {
    server.value = await getServer(serverId.value);
  } catch (err) {
    server.value = null;
    error.value = describeServerError(err);
  } finally {
    loading.value = false;
  }
}

/** handleValidate runs the SSH probes and refreshes the header. */
async function handleValidate(): Promise<void> {
  if (!server.value) {
    return;
  }
  validating.value = true;
  try {
    const outcome = await serversStore.validate(server.value.id);
    if (outcome.server) {
      server.value = outcome.server;
    }
    if (outcome.ok) {
      message.success(`${server.value.name}: validation passed`);
      return;
    }
    const failed = outcome.checks
      .filter((check) => !check.ok)
      .map((check) => check.name)
      .join(", ");
    message.error(
      outcome.message || `${server.value.name}: failed checks: ${failed}`,
    );
  } catch (err) {
    message.error(describeServerError(err));
  } finally {
    validating.value = false;
  }
}

/** handleDelete removes the server and returns to the list. */
async function handleDelete(): Promise<void> {
  if (!server.value) {
    return;
  }
  const name = server.value.name;
  deleting.value = true;
  try {
    await serversStore.removeServer(server.value.id);
    message.success(`Deleted ${name}`);
    await router.push({ name: "servers" });
  } catch (err) {
    message.error(describeServerError(err));
  } finally {
    deleting.value = false;
  }
}

watch(serverId, () => {
  activeTab.value = "overview";
  void fetchServer();
});

onMounted(() => {
  void fetchServer();
});
</script>

<template>
  <NSpace vertical :size="16">
    <nav class="breadcrumb" aria-label="Breadcrumb">
      <RouterLink to="/servers">Servers</RouterLink>
      <span class="breadcrumb__sep">/</span>
      <span class="muted">{{ server?.name ?? serverId }}</span>
    </nav>

    <NSpin :show="loading">
      <NAlert
        v-if="error"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ error }}
      </NAlert>

      <template v-if="server">
        <div class="page-head">
          <NAvatar round :size="48">{{ initials }}</NAvatar>
          <div class="page-head__title">
            <NSpace align="center" :size="10">
              <NText strong style="font-size: 20px">{{ server.name }}</NText>
              <ServerStatusTag :status="server.status" size="medium" />
            </NSpace>
            <NText depth="3">{{ summaryLine }}</NText>
          </div>
          <NSpace class="page-head__actions" align="center" :size="8">
            <NButton :loading="validating" @click="handleValidate">
              Validate
            </NButton>
            <RouterLink
              :to="{ name: 'server-containers', params: { id: server.id } }"
              custom
            >
              <template #default="{ navigate }">
                <NButton type="primary" @click="navigate">
                  Open containers
                </NButton>
              </template>
            </RouterLink>
          </NSpace>
        </div>

        <NTabs v-model:value="activeTab" type="line" animated>
          <NTabPane name="overview" tab="Overview">
            <NSpace vertical :size="16" style="margin-top: 16px">
              <NCard title="Node info">
                <NDescriptions :column="2" bordered label-placement="left">
                  <NDescriptionsItem label="Name">
                    {{ server.name }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Address">
                    <span class="mono">{{ server.ip }}:{{ server.port }}</span>
                  </NDescriptionsItem>
                  <NDescriptionsItem label="SSH user">
                    <span class="mono">{{ server.ssh_user }}</span>
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Node ID">
                    <span class="mono">{{ fallback(server.node_id) }}</span>
                  </NDescriptionsItem>
                  <NDescriptionsItem label="OS">
                    {{ fallback(server.os) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Architecture">
                    {{ fallback(server.arch) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Docker">
                    {{ fallback(server.docker_version) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="SSH key">
                    <span class="mono">{{ fallback(server.ssh_key_id) }}</span>
                  </NDescriptionsItem>
                  <NDescriptionsItem label="CPU usage">
                    {{ usageText(server.cpu_usage) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Memory usage">
                    {{ usageText(server.mem_usage) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Disk usage">
                    {{ usageText(server.disk_usage) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Containers">
                    {{ server.container_count ?? "—" }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Last seen">
                    {{ relativeTime(server.last_seen) }}
                  </NDescriptionsItem>
                  <NDescriptionsItem label="Registered">
                    {{ relativeTime(server.created_at) }}
                  </NDescriptionsItem>
                </NDescriptions>
              </NCard>

              <NCard title="Labels">
                <NEmpty description="No labels on this node yet." />
              </NCard>

              <NCard title="Danger zone">
                <NText depth="3">
                  Deleting a node removes it from the control plane only.
                  Containers, volumes and certificates on the machine are kept.
                </NText>
                <div style="margin-top: 12px">
                  <NPopconfirm
                    :positive-button-props="{ type: 'error' }"
                    @positive-click="handleDelete"
                  >
                    <template #trigger>
                      <NButton type="error" ghost :loading="deleting">
                        Delete node
                      </NButton>
                    </template>
                    Delete server "{{ server.name }}"?
                  </NPopconfirm>
                </div>
              </NCard>
            </NSpace>
          </NTabPane>

          <NTabPane name="containers" tab="Containers">
            <NCard style="margin-top: 16px">
              <NEmpty
                description="Container management lives on the containers page."
              >
                <template #extra>
                  <RouterLink
                    :to="{
                      name: 'server-containers',
                      params: { id: server.id },
                    }"
                    custom
                  >
                    <template #default="{ navigate }">
                      <NButton type="primary" @click="navigate">
                        Open containers
                      </NButton>
                    </template>
                  </RouterLink>
                </template>
              </NEmpty>
            </NCard>
          </NTabPane>

          <NTabPane name="metrics" tab="Metrics">
            <NCard style="margin-top: 16px">
              <NEmpty description="Metrics ship in Phase 8." />
            </NCard>
          </NTabPane>

          <NTabPane name="proxy" tab="Proxy & Traefik">
            <NCard style="margin-top: 16px">
              <NEmpty description="Proxy & Traefik ships in Phase 6." />
            </NCard>
          </NTabPane>

          <NTabPane name="settings" tab="Node settings">
            <NCard title="Node settings" style="margin-top: 16px">
              <NText depth="3" style="display: block; margin-bottom: 12px">
                Editable settings do not exist in the backend yet. Values below
                are read-only.
              </NText>
              <NDescriptions :column="2" bordered label-placement="left">
                <NDescriptionsItem label="Name">
                  {{ server.name }}
                </NDescriptionsItem>
                <NDescriptionsItem label="Address">
                  <span class="mono">{{ server.ip }}:{{ server.port }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="SSH user">
                  <span class="mono">{{ server.ssh_user }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="SSH key">
                  <span class="mono">{{ fallback(server.ssh_key_id) }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Registered">
                  {{ relativeTime(server.created_at) }}
                </NDescriptionsItem>
                <NDescriptionsItem label="Updated">
                  {{ relativeTime(server.updated_at) }}
                </NDescriptionsItem>
              </NDescriptions>
            </NCard>
          </NTabPane>
        </NTabs>
      </template>
    </NSpin>
  </NSpace>
</template>

<style scoped>
.breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
}

.breadcrumb__sep {
  color: var(--meta);
}

.page-head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
}

.page-head__title {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  flex: 1;
  min-width: 0;
}

.page-head__actions {
  margin-left: auto;
  flex-shrink: 0;
}
</style>
