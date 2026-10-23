export interface ServerListToken {
  readonly request: number;
  readonly mutation: number;
}

export interface ServerListSync {
  begin(): ServerListToken;
  admit(_token: ServerListToken): boolean;
  markMutation(): void;
}

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
