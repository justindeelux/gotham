import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  createChannel,
  deleteChannel,
  describeChannelError,
  isFeatureDisabled,
  listChannels,
  listDeliveries,
  testChannel,
  updateChannel,
} from "@/features/notifications/api/notifications";
import type {
  ChannelInput,
  NotificationChannel,
  NotificationDelivery,
  TestResult,
} from "@/features/notifications/api/notifications";
import { useTeamsStore } from "@/features/teams";

export const useNotificationsStore = defineStore("notifications", () => {
  const channels = ref<NotificationChannel[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const errorRaw = ref<unknown>(null);
  const error = computed<string | null>(() =>
    errorRaw.value === null ? null : describeChannelError(errorRaw.value),
  );
  const featureDisabled = ref(false);

  const loadedTeamId = ref("");

  let listReadToken = 0;

  function activeTeamId(): string {
    return useTeamsStore().activeTeamId;
  }

  function clearList(): void {
    channels.value = [];
    loaded.value = false;
    errorRaw.value = null;
    featureDisabled.value = false;
  }

  function applyChannel(updated: NotificationChannel): void {
    if (updated.team_id !== loadedTeamId.value) {
      return;
    }
    const index = channels.value.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      channels.value = [...channels.value, updated];
      return;
    }
    channels.value[index] = updated;
  }

  async function fetchChannels(): Promise<void> {
    const teamId = activeTeamId();
    const token = ++listReadToken;
    const isCurrent = (): boolean =>
      token === listReadToken && activeTeamId() === teamId;

    if (loadedTeamId.value !== teamId) {
      clearList();
      loadedTeamId.value = teamId;
    }
    loading.value = true;
    errorRaw.value = null;
    try {
      const next = await listChannels(teamId);
      if (!isCurrent()) {
        return;
      }
      channels.value = next;
      featureDisabled.value = false;
      loaded.value = true;
    } catch (err) {
      if (!isCurrent()) {
        return;
      }
      if (isFeatureDisabled(err)) {
        featureDisabled.value = true;
        loaded.value = true;
        channels.value = [];
        return;
      }
      errorRaw.value = err;
      throw err;
    } finally {
      if (isCurrent()) {
        loading.value = false;
      }
    }
  }

  async function create(input: ChannelInput): Promise<NotificationChannel> {
    const channel = await createChannel(activeTeamId(), input);
    applyChannel(channel);
    return channel;
  }

  async function update(
    id: string,
    input: ChannelInput,
  ): Promise<NotificationChannel> {
    const channel = await updateChannel(activeTeamId(), id, input);
    applyChannel(channel);
    return channel;
  }

  async function remove(id: string): Promise<void> {
    const teamId = activeTeamId();
    await deleteChannel(teamId, id);
    if (loadedTeamId.value !== teamId) {
      return;
    }
    channels.value = channels.value.filter((item) => item.id !== id);
  }

  function test(id: string): Promise<TestResult> {
    return testChannel(activeTeamId(), id);
  }

  function history(id: string): Promise<NotificationDelivery[]> {
    return listDeliveries(activeTeamId(), id);
  }

  function reset(): void {
    listReadToken += 1;
    clearList();
    loadedTeamId.value = "";
    loading.value = false;
  }

  return {
    channels,
    loading,
    loaded,
    error,
    featureDisabled,
    fetchChannels,
    create,
    update,
    remove,
    test,
    history,
    reset,
  };
});
