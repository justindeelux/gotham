import { defineStore } from "pinia";
import { ref } from "vue";

import {
  createChannel,
  deleteChannel,
  describeChannelError,
  isFeatureDisabled,
  listChannels,
  testChannel,
  updateChannel,
} from "../api/notifications";
import type {
  ChannelInput,
  NotificationChannel,
  TestResult,
} from "../api/notifications";
import { useTeamsStore } from "./teams";

/**
 * Notification channels of the active team.
 *
 * Every call is scoped to the team selected in the teams store (`X-Team-Id`,
 * the personal team when none is selected), so switching teams reloads the
 * list instead of showing another team's channels. A read that never
 * succeeded is distinguishable from a genuinely empty list: `loaded` is only
 * set by a successful response and `featureDisabled` marks the
 * FEATURE_NOTIFICATIONS=false 404, which the page renders as a hidden surface
 * rather than an error.
 */
export const useNotificationsStore = defineStore("notifications", () => {
  const channels = ref<NotificationChannel[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const error = ref<string | null>(null);
  const featureDisabled = ref(false);

  /** activeTeamId reads the current team selection. */
  function activeTeamId(): string {
    return useTeamsStore().activeTeamId;
  }

  /** applyChannel merges one channel into the list in place. */
  function applyChannel(updated: NotificationChannel): void {
    const index = channels.value.findIndex((item) => item.id === updated.id);
    if (index === -1) {
      channels.value = [...channels.value, updated];
      return;
    }
    channels.value[index] = updated;
  }

  /** fetchChannels loads the active team's channels. */
  async function fetchChannels(): Promise<void> {
    const teamId = activeTeamId();
    loading.value = true;
    error.value = null;
    try {
      channels.value = await listChannels(teamId);
      featureDisabled.value = false;
      loaded.value = true;
    } catch (err) {
      if (isFeatureDisabled(err)) {
        featureDisabled.value = true;
        loaded.value = true;
        channels.value = [];
        return;
      }
      error.value = describeChannelError(err);
      throw err;
    } finally {
      loading.value = false;
    }
  }

  /** create stores a channel in the active team. */
  async function create(input: ChannelInput): Promise<NotificationChannel> {
    const channel = await createChannel(activeTeamId(), input);
    applyChannel(channel);
    return channel;
  }

  /** update patches one channel; omitted fields stay unchanged. */
  async function update(
    id: string,
    input: ChannelInput,
  ): Promise<NotificationChannel> {
    const channel = await updateChannel(activeTeamId(), id, input);
    applyChannel(channel);
    return channel;
  }

  /** remove deletes one channel and drops it from the list. */
  async function remove(id: string): Promise<void> {
    await deleteChannel(activeTeamId(), id);
    channels.value = channels.value.filter((item) => item.id !== id);
  }

  /** test delivers a synthetic event through one channel. */
  function test(id: string): Promise<TestResult> {
    return testChannel(activeTeamId(), id);
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
  };
});
