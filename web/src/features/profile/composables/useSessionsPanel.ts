import { useMessage } from "naive-ui";
import { computed, ref } from "vue";
import { useRouter } from "vue-router";

import { useAuthStore } from "@/features/auth";
import {
  listSessions,
  revokeOtherSessions,
  revokeSession,
} from "@/features/profile/api/sessions";
import { profileMessages } from "@/features/profile/schemas/profile";
import type { AuthSession } from "@/features/profile/schemas/sessions";

// Error convention (shared with the profile forms): submit failures render
// once in the NAlert above the panel. A 404 on revoke means the session is
// already gone, so it refreshes silently instead of erroring.

/**
 * useSessionsPanel holds the active-sessions panel state. Created per panel
 * mount and dropped on unmount, so a remount always starts from a fresh load
 * and never shows a stale list.
 */
export function useSessionsPanel() {
  const authStore = useAuthStore();
  const message = useMessage();
  const router = useRouter();

  const sessions = ref<AuthSession[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const errorMessage = ref("");
  const revokingId = ref<string | null>(null);
  const revokingOthers = ref(false);
  // needsReauth marks the 409 path: the token predates session management,
  // so the server cannot tell which session is current.
  const needsReauth = ref(false);

  /** others lists every session except the caller's. */
  const others = computed<AuthSession[]>(() =>
    sessions.value.filter((session) => !session.current),
  );

  async function load(): Promise<void> {
    if (loading.value) {
      return;
    }
    loading.value = true;
    errorMessage.value = "";
    try {
      sessions.value = await listSessions();
      loaded.value = true;
    } catch {
      errorMessage.value = profileMessages.sessionsLoadFailed;
    } finally {
      loading.value = false;
    }
  }

  /** statusOf extracts the HTTP status from a thrown ApiError, if present. */
  function statusOf(error: unknown): number | null {
    if (typeof error !== "object" || error === null) {
      return null;
    }
    const status = (error as { status?: unknown }).status;
    return typeof status === "number" ? status : null;
  }

  /** signOutHere ends the local session and returns to the sign-in page. */
  async function signOutHere(): Promise<void> {
    try {
      await authStore.logout();
    } finally {
      await router.push({ name: "login" });
    }
  }

  /**
   * endSession revokes one session and refreshes the list. Ending the
   * current session goes through the existing sign-out path so no half-valid
   * session is left behind; an unknown id (404) is already gone, so it
   * refreshes silently.
   */
  async function endSession(session: AuthSession): Promise<void> {
    if (revokingId.value !== null) {
      return;
    }
    revokingId.value = session.id;
    errorMessage.value = "";
    try {
      await revokeSession(session.id);
      if (session.current) {
        message.success(profileMessages.sessionsSignedOutHere);
        await signOutHere();
        return;
      }
      message.success(profileMessages.sessionSignedOut);
      sessions.value = await listSessions();
    } catch (error) {
      if (statusOf(error) === 404) {
        sessions.value = await listSessions().catch(() => sessions.value);
        return;
      }
      errorMessage.value = profileMessages.sessionEndFailed;
    } finally {
      revokingId.value = null;
    }
  }

  /**
   * endOtherSessions revokes every session except the caller's and refreshes
   * the list. A 409 means the token predates session management: the panel
   * shows the re-auth explanation with a "Sign in again" action instead.
   */
  async function endOtherSessions(): Promise<void> {
    if (revokingOthers.value) {
      return;
    }
    revokingOthers.value = true;
    errorMessage.value = "";
    try {
      await revokeOtherSessions();
      message.success(profileMessages.sessionsSignedOut);
      sessions.value = await listSessions();
    } catch (error) {
      if (statusOf(error) === 409) {
        needsReauth.value = true;
        return;
      }
      errorMessage.value = profileMessages.revokeOthersFailed;
    } finally {
      revokingOthers.value = false;
    }
  }

  return {
    sessions,
    others,
    loading,
    loaded,
    errorMessage,
    revokingId,
    revokingOthers,
    needsReauth,
    load,
    endSession,
    endOtherSessions,
    signOutHere,
  };
}
