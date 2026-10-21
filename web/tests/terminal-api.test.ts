// JUS-248: backend terminal failures are mapped to stable, translatable
// classes. The control plane's contract is the input: HTTP statuses for
// create, bounded phrases for the attach stream.
import { describe, expect, it } from "vitest";

import { ZodError } from "zod";

import {
  applicationTerminalFailure,
  terminalFailure,
  terminalStreamFailureCode,
} from "@/features/terminal/api/terminal";
import {
  applicationTerminalTargetsSchema,
  terminalSessionSchema,
  terminalTargetsSchema,
} from "@/features/terminal/schemas/terminal";

// Literal create responses from the control plane's create contract. They are
// kept as JSON strings so a backend shape drift is visible here.
const HOST_NO_ROOT_RESPONSE = {
  id: "3f8c2a1e-0000-4000-8000-000000000001",
  server_id: "11111111-1111-4111-8111-111111111111",
  target: "host",
  cols: 120,
  rows: 32,
  expires_at: "2026-10-20T12:01:00Z",
  websocket_path:
    "/api/v1/servers/11111111-1111-4111-8111-111111111111/terminal/sessions/3f8c2a1e-0000-4000-8000-000000000001/ws",
  subprotocol: "gotham.terminal.v1",
} as const;

const HOST_WITH_ROOT_RESPONSE = {
  ...HOST_NO_ROOT_RESPONSE,
  root: "volumes",
  cwd: "my-app/data",
} as const;

const CONTAINER_RESPONSE = {
  id: "3f8c2a1e-0000-4000-8000-000000000002",
  server_id: "11111111-1111-4111-8111-111111111111",
  target: "container",
  cols: 120,
  rows: 32,
  expires_at: "2026-10-20T12:01:00Z",
  websocket_path:
    "/api/v1/applications/22222222-2222-4222-8222-222222222222/terminal/sessions/3f8c2a1e-0000-4000-8000-000000000002/ws",
  subprotocol: "gotham.terminal.v1",
  application_id: "22222222-2222-4222-8222-222222222222",
  service: "web",
  container_id: "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
  container_name: "gotham-22222222-web-1",
  target_label: "web · gotham-22222222-web-1",
} as const;

describe("terminalSessionSchema", () => {
  it("accepts a host create response without a File manager root", () => {
    // The Terminal page and the server detail tab create a host session with
    // no root/cwd; the control plane omits both fields in that response.
    const parsed = terminalSessionSchema.parse(HOST_NO_ROOT_RESPONSE);
    expect(parsed.root).toBe("");
    expect(parsed.cwd).toBe("");
  });

  it("keeps an explicit root and cwd", () => {
    const parsed = terminalSessionSchema.parse(HOST_WITH_ROOT_RESPONSE);
    expect(parsed.root).toBe("volumes");
    expect(parsed.cwd).toBe("my-app/data");
  });

  it("accepts the container create response", () => {
    const parsed = terminalSessionSchema.parse(CONTAINER_RESPONSE);
    expect(parsed.target).toBe("container");
    expect(parsed.root).toBe("");
    expect(parsed.cwd).toBe("");
  });

  it("rejects an unexpected 2xx shape", () => {
    expect(() => terminalSessionSchema.parse({ id: "only-an-id" })).toThrow(
      ZodError,
    );
  });
});

describe("terminalFailure", () => {
  it("reports an unexpected 2xx response as badResponse, not network", () => {
    expect(terminalFailure(new ZodError([]))).toEqual({
      code: "badResponse",
      message: "",
    });
  });

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
        "the node agent needs an update to open a terminal without a folder",
      ),
    ).toBe("agentUpdate");
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


describe("applicationTerminalFailure (JUS-247)", () => {
  it("maps the container statuses and the 409 phrases", () => {
    expect(applicationTerminalFailure({ status: 403, message: "x" }).code).toBe(
      "applicationForbidden",
    );
    expect(applicationTerminalFailure({ status: 404, message: "not found" }).code).toBe(
      "applicationNotFound",
    );
    expect(
      applicationTerminalFailure({
        status: 404,
        message: "the selected container is no longer available",
      }).code,
    ).toBe("containerGone");
    expect(
      applicationTerminalFailure({
        status: 409,
        message: "the application has no running deployment",
      }).code,
    ).toBe("noDeployment");
    expect(
      applicationTerminalFailure({
        status: 409,
        message: "a deployment is in progress; try again when it finishes",
      }).code,
    ).toBe("deployInFlight");
    expect(
      applicationTerminalFailure({
        status: 409,
        message: "the selected container is not running; start the application first",
      }).code,
    ).toBe("containerStopped");
    expect(applicationTerminalFailure({ status: 502, message: "x" }).code).toBe(
      "unavailable",
    );
    expect(applicationTerminalFailure({ status: 503, message: "x" }).code).toBe(
      "terminalDisabled",
    );
    // An unexpected 2xx target/create shape is badResponse, never network.
    expect(applicationTerminalFailure(new ZodError([])).code).toBe(
      "badResponse",
    );
  });
});

describe("applicationTerminalTargetsSchema (JUS-247)", () => {
  it("accepts the Go wire shape, including an empty single-app service", () => {
    const parsed = applicationTerminalTargetsSchema.safeParse({
      application_id: "app-1",
      application_name: "shop",
      server_id: "srv-1",
      source_type: "image",
      containers: [
        {
          service: "",
          container_id: "cid-1",
          name: "gotham-shop-1",
          image: "nginx:1",
          state: "running",
          replica: 1,
          label: "gotham-shop-1",
        },
      ],
    });
    expect(parsed.success).toBe(true);
  });

  it("rejects a drifted envelope without containers", () => {
    expect(
      applicationTerminalTargetsSchema.safeParse({
        application_id: "app-1",
        server_id: "srv-1",
      }).success,
    ).toBe(false);
  });
});

describe("terminalTargetsSchema (JUS-265)", () => {
  // The backend shape, with every optional field omitted where the control
  // plane omits it: target errors carry only source+code.
  const TARGETS = {
    servers: [
      { id: "11111111-1111-4111-8111-111111111111", name: "prod-01", status: "ready" },
    ],
    truncated: false,
    applications: [
      {
        application_id: "22222222-2222-4222-8222-222222222222",
        application_name: "shop",
        server_id: "11111111-1111-4111-8111-111111111111",
        source_type: "image",
        containers: [
          {
            service: "web",
            container_id: "a".repeat(64),
            name: "shop-web-1",
            image: "nginx:1.28.0-alpine",
            state: "running",
            replica: 1,
            label: "web · shop-web-1",
          },
        ],
      },
    ],
    errors: [{ source: "application", code: "unavailable" }],
  } as const;

  it("accepts the backend shape with optional source fields omitted", () => {
    const parsed = terminalTargetsSchema.parse(TARGETS);
    expect(parsed.servers).toHaveLength(1);
    expect(parsed.applications[0]?.containers[0]?.container_id).toBe("a".repeat(64));
    expect(parsed.errors[0]).toEqual({ source: "application", code: "unavailable" });
  });

  it("accepts empty lists (a caller with no targets)", () => {
    expect(
      terminalTargetsSchema.parse({
        servers: [],
        applications: [],
        errors: [],
        truncated: false,
      }),
    ).toEqual({ servers: [], applications: [], errors: [], truncated: false });
  });

  it("rejects a drifted body instead of rendering a half-valid list", () => {
    expect(() => terminalTargetsSchema.parse({ servers: [] })).toThrow(ZodError);
    expect(() =>
      terminalTargetsSchema.parse({
        ...TARGETS,
        errors: [{ source: "application", code: "raw backend text" }],
      }),
    ).toThrow(ZodError);
    expect(() =>
      terminalTargetsSchema.parse({
        ...TARGETS,
        applications: [
          {
            ...TARGETS.applications[0],
            containers: [{ ...TARGETS.applications[0].containers[0], container_id: "" }],
          },
        ],
      }),
    ).toThrow(ZodError);
  });
});
