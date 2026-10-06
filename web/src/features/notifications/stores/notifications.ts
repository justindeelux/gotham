import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  createChannel,
  deleteChannel,
  describeChannelError,
  isFeatureDisabled,
  listChannels,
  testChannel,
  updateChannel,
} from "@/features/notifications/api/notifications";
import type {
  ChannelInput,
  NotificationChannel,
  TestResult,
} from "@/features/notifications/api/notifications";
import { useTeamsStore } from "@/features/teams";

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
 *
 * Reads and mutations are race-guarded by team: a response may only write
 * state while it still belongs to the team the list currently holds
 * (`loadedTeamId`), and only the newest list read may write at all. A read
 * for another team clears the list first, so the previous team's channels can
 * never render under a new selection — not even after a failed read.
 */
export const useNotificationsStore = defineStore("notifications", () => {
  const channels = ref<NotificationChannel[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  /**
   * Raw failure behind the page alert. The display string derives from it
   * plus the current locale, so a language switch refreshes a retained
   * banner without a refetch; classification always sees the raw error.
   */
  const errorRaw = ref<unknown>(null);
  const error = computed<string | null>(() =>
    errorRaw.value === null ? null : describeChannelError(errorRaw.value),
  );
  const featureDisabled = ref(false);

  /** Team the current list belongs to; empty when no list is held. */
  const loadedTeamId = ref("");

  /** Token of the newest list read; a stale response never writes state. */
  let listReadToken = 0;

  /** activeTeamId reads the current team selection. */
  function activeTeamId(): string {
    return useTeamsStore().activeTeamId;
  }

  /** clearList drops the held list (used when the team changes). */
  function clearList(): void {
    channels.value = [];
    loaded.value = false;
    errorRaw.value = null;
    featureDisabled.value = false;
  }

  /**
   * applyChannel merges one channel into the list in place, but only when it
   * belongs to the team the list holds: a mutation that completes after the
   * operator switched teams must not leak into the new team's list.
   */
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

  /** fetchChannels loads the active team's channels. */
  async function fetchChannels(): Promise<void> {
    const teamId = activeTeamId();
    const token = ++listReadToken;
    const isCurrent = (): boolean =>
      token === listReadToken && activeTeamId() === teamId;

    if (loadedTeamId.value !== teamId) {
      // A different team's rows must never render under this selection, and
      // any read still in flight for the old team is now obsolete.
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
      // Only the newest read owns the spinner; an obsolete one must not clear
      // a loading state the current read still needs.
      if (isCurrent()) {
        loading.value = false;
      }
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
    const teamId = activeTeamId();
    await deleteChannel(teamId, id);
    if (loadedTeamId.value !== teamId) {
      return;
    }
    channels.value = channels.value.filter((item) => item.id !== id);
  }

  /** test delivers a synthetic event through one channel. */
  function test(id: string): Promise<TestResult> {
    return testChannel(activeTeamId(), id);
  }

  /**
   * reset drops the held list and its team binding, so the next sign-in
   * never sees the previous account's channels. Called on sign-out (see the
   * auth store).
   */
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
    reset,
  };
});
