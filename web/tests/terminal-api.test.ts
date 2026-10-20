// JUS-248: backend terminal failures are mapped to stable, translatable
// classes. The control plane's contract (docs/terminal-session-api.md) is the
// input: HTTP statuses for create, bounded phrases for the attach stream.
import { describe, expect, it } from "vitest";

import {
  terminalFailure,
  terminalStreamFailureCode,
} from "@/features/terminal/api/terminal";

describe("terminalFailure", () => {
  it("maps the create-session statuses the file manager can hit", () => {
    expect(terminalFailure({ status: 401, message: "unauthorized" })).toEqual({
      code: "auth",
      message: "unauthorized",
    });
    expect(terminalFailure({ status: 403, message: "forbidden" })).toEqual({
      code: "forbidden",
      message: "forbidden",
    });
    expect(terminalFailure({ status: 404, message: "not found" })).toEqual({
      code: "node",
      message: "not found",
    });
    expect(terminalFailure({ status: 429, message: "too many" })).toEqual({
      code: "quota",
      message: "too many",
    });
    expect(terminalFailure({ status: 501, message: "unsupported" })).toEqual({
      code: "unsupported",
      message: "unsupported",
    });
    expect(terminalFailure({ status: 503, message: "unavailable" })).toEqual({
      code: "unavailable",
      message: "unavailable",
    });
    expect(terminalFailure(new Error("offline"))).toEqual({
      code: "network",
      message: "offline",
    });
  });

  it("recognises the contract's invalid-working-directory 400", () => {
    expect(
      terminalFailure({
        status: 400,
        message: "terminal: invalid request: invalid working directory",
      }),
    ).toEqual({
      code: "cwdInvalid",
      message: "terminal: invalid request: invalid working directory",
    });
    expect(terminalFailure({ status: 400, message: "invalid request body" })).toEqual({
      code: "generic",
      message: "invalid request body",
    });
  });
});

describe("terminalStreamFailureCode", () => {
  it("maps every working-directory phrase the relay sends", () => {
    expect(terminalStreamFailureCode("invalid working directory")).toBe(
      "cwdInvalid",
    );
    expect(
      terminalStreamFailureCode("the working directory no longer exists"),
    ).toBe("cwdMissing");
    expect(
      terminalStreamFailureCode("the working directory is not readable"),
    ).toBe("cwdUnreadable");
  });

  it("maps agent and quota phrases, ignoring case and padding", () => {
    expect(
      terminalStreamFailureCode(
        "  The Node Agent Does Not Support Terminal Sessions  ",
      ),
    ).toBe("unsupported");
    expect(
      terminalStreamFailureCode(
        "the node is running the maximum number of terminal sessions",
      ),
    ).toBe("quota");
    expect(terminalStreamFailureCode("the node agent is unavailable")).toBe(
      "unavailable",
    );
  });

  it("leaves an unknown bounded message verbatim", () => {
    expect(terminalStreamFailureCode("agent offline")).toBeNull();
  });
});
