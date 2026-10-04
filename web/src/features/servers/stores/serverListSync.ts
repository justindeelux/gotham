/**
 * Ordering guard shared by the servers store.
 *
 * The servers list is refreshed by a five-second poll while mutations (create,
 * validate, delete) can land at any time. Without ordering, an older poll
 * response that resolves late replaces the whole list, resurrecting a deleted
 * row or clobbering a fresh validation merge (A4-16/B4-5). The guard hands out
 * a token per load and only admits the newest token that no mutation has
 * invalidated.
 */
export interface ServerListToken {
  /** Monotonic id of the load this token belongs to. */
  readonly request: number;
  /** Mutation counter captured when the load began. */
  readonly mutation: number;
}

export interface ServerListSync {
  /** begin starts a load and returns the token its response must present. */
  begin(): ServerListToken;
  /**
   * admit records the token as applied and reports whether its response may
   * replace local state. It rejects a response older than one already applied,
   * and any response whose load began before the last mutation.
   */
  admit(_token: ServerListToken): boolean;
  /** markMutation invalidates every load that started before it. */
  markMutation(): void;
}

/** createServerListSync builds an independent guard instance. */
export function createServerListSync(): ServerListSync {
  let nextRequest = 0;
  let applied = 0;
  let mutations = 0;

  return {
    begin(): ServerListToken {
      return { request: ++nextRequest, mutation: mutations };
    },
    admit(token: ServerListToken): boolean {
      if (token.request <= applied) {
        return false;
      }
      if (token.mutation !== mutations) {
        return false;
      }
      applied = token.request;
      return true;
    },
    markMutation(): void {
      mutations += 1;
    },
  };
}
