import { useMessage } from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import type { ComputedRef } from "vue";
import { useI18n } from "vue-i18n";

import type { OrphanedVolume } from "@/features/servers/api/orphanedVolumes";
import {
  listOrphanedVolumes,
  removeOrphanedVolume,
} from "@/features/servers/api/orphanedVolumes";
import { describeContainerError } from "@/features/servers/api/containers";

export function useOrphanedVolumes(serverId: ComputedRef<string>) {
  const message = useMessage();
  const { t } = useI18n();

  const volumes = ref<OrphanedVolume[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const fetchFailure = ref<unknown>(null);
  const error = computed<string | null>(() =>
    fetchFailure.value === null || fetchFailure.value === undefined
      ? null
      : describeContainerError(fetchFailure.value),
  );

  const removing = ref<string | null>(null);

  async function fetchVolumes(showLoading: boolean): Promise<void> {
    if (!serverId.value) {
      return;
    }
    if (showLoading) {
      loading.value = true;
    }
    try {
      volumes.value = await listOrphanedVolumes(serverId.value);
      fetchFailure.value = null;
      loaded.value = true;
    } catch (err) {
      fetchFailure.value = err;
    } finally {
      loading.value = false;
    }
  }

  async function removeVolume(name: string): Promise<void> {
    removing.value = name;
    try {
      await removeOrphanedVolume(serverId.value, name);
      message.success(t("servers.volumes.removed", { name }));
      await fetchVolumes(false);
    } catch (err) {
      message.error(describeContainerError(err));
    } finally {
      removing.value = null;
    }
  }

  watch(
    () => serverId.value,
    () => {
      loaded.value = false;
      void fetchVolumes(true);
    },
  );

  onMounted(() => {
    void fetchVolumes(true);
  });

  return {
    volumes,
    loading,
    loaded,
    error,
    removing,
    fetchVolumes,
    removeVolume,
  };
}
