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

// Error convention (shared with the profile forms): failures render in the
// NAlert above the panel, never replacing the list. A 404 on revoke means
// the session is already gone, so it refreshes silently instead of erroring.

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

  // listSeq orders every list write. load and the post-revoke refreshes race
  // (retry while a refresh is in flight, end-one vs end-others), so only the
  // latest response may replace the list.
  let listSeq = 0;

  /** others lists every session except the caller's. */
  const others = computed<AuthSession[]>(() =>
    sessions.value.filter((session) => !session.current),
  );

  async function load(): Promise<void> {
    const seq = ++listSeq;
    loading.value = true;
    errorMessage.value = "";
    try {
      const list = await listSessions();
      if (seq !== listSeq) {
        return;
      }
      sessions.value = list;
      loaded.value = true;
    } catch {
      if (seq !== listSeq) {
        return;
      }
      errorMessage.value = profileMessages.sessionsLoadFailed;
    } finally {
      if (seq === listSeq) {
        loading.value = false;
      }
    }
  }

  /**
   * refreshList reloads the list after a successful revoke. It runs outside
   * the revoke try/catch: a failing refresh must not report a failed revoke.
   * Returns false when the refresh failed (the caller then drops the row
   * locally and shows the stale notice); a superseded response is true.
   */
  async function refreshList(seq: number): Promise<boolean> {
    try {
      const list = await listSessions();
      if (seq === listSeq) {
        sessions.value = list;
      }
      return true;
    } catch {
      return false;
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
    } catch {
      // logout clears the local session in `finally`; a failed revoke must
      // not block the redirect or surface as an unhandled rejection (B3-3,
      // like AppTopbar/MeCard).
    }
    await router.push({ name: "login" });
  }

  /**
   * endSession revokes one session and refreshes the list. Ending the
   * current session goes through the existing sign-out path so no half-valid
   * session is left behind; an unknown id (404) is already gone, so it
   * refreshes silently. A refresh that fails after a successful revoke drops
   * the row locally and shows the stale notice instead of a revoke error.
   */
  async function endSession(session: AuthSession): Promise<void> {
    if (revokingId.value !== null) {
      return;
    }
    revokingId.value = session.id;
    errorMessage.value = "";
    const seq = ++listSeq;
    try {
      await revokeSession(session.id);
    } catch (error) {
      revokingId.value = null;
      if (statusOf(error) === 404) {
        await refreshList(seq);
        return;
      }
      errorMessage.value = profileMessages.sessionEndFailed;
      return;
    }
    revokingId.value = null;
    if (session.current) {
      message.success(profileMessages.sessionsSignedOutHere);
      await signOutHere();
      return;
    }
    message.success(profileMessages.sessionSignedOut);
    if (!(await refreshList(seq))) {
      sessions.value = sessions.value.filter((row) => row.id !== session.id);
      errorMessage.value = profileMessages.sessionsListStale;
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
    const seq = ++listSeq;
    try {
      await revokeOtherSessions();
    } catch (error) {
      revokingOthers.value = false;
      if (statusOf(error) === 409) {
        needsReauth.value = true;
        return;
      }
      errorMessage.value = profileMessages.revokeOthersFailed;
      return;
    }
    revokingOthers.value = false;
    message.success(profileMessages.sessionsSignedOut);
    if (!(await refreshList(seq))) {
      sessions.value = sessions.value.filter((row) => row.current);
      errorMessage.value = profileMessages.sessionsListStale;
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
