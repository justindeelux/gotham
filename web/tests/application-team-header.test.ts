/* global window:readonly */
import type { AxiosResponse, InternalAxiosRequestConfig } from "axios";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { http } from "@/shared/api/http";
import { activeTeamStorageKey } from "@/shared/api/token";
import {
  addDomain,
  createApplication,
  createDeployKey,
  deleteApplication,
  deleteDeployKey,
  deleteGitCredential,
  getApplication,
  getDeployKey,
  getDeploymentBuildLog,
  getEnv,
  getGitCredential,
  getStorages,
  listApplications,
  listDeployments,
  listDomains,
  listRecentDeployments,
  removeDomain,
  replaceEnv,
  replaceStorages,
  rollbackDeployment,
  setGitCredential,
  setPrimaryDomain,
  startApplication,
  stopApplication,
  testConnection,
  triggerDeploy,
  updateApplication,
  updateDomain,
} from "@/features/applications/api/applications";
import type {
  CreateApplicationInput,
  DomainInput,
} from "@/features/applications/api/applications";
import { listPreviews } from "@/features/applications/api/previews";
import {
  closeApplicationTerminalSession,
  createApplicationTerminalSession,
  listApplicationTerminalTargets,
} from "@/features/terminal/api/terminal";

const teamId = "team-shared";
const appId = "app-1";

let captured: InternalAxiosRequestConfig[] = [];
const originalAdapter = http.defaults.adapter;

const payload = {
  application: {
    id: appId,
    name: "shop",
    environment_id: "env-1",
    environment_name: "production",
    project_id: "proj-1",
    project_name: "storefront",
    source_type: "image",
    image_ref: "nginx:1",
    server_id: "srv-1",
    server_name: "prod-01",
  },
  applications: [],
  deployment: { id: "dep-1", application_id: appId, state: "running" },
  deployments: [],
  env: [],
  storage: [],
  domain: { id: "dom-1", application_id: appId, domain: "shop.example.com" },
  domains: [],
  deploy_key: { id: "key-1", application_id: appId, public_key: "ssh-ed25519" },
  log: "",
  deleted: true,
  has_credential: false,
  ok: true,
  previews: [],
  application_id: appId,
  application_name: "shop",
  server_id: "srv-1",
  source_type: "image",
  containers: [],
  id: "session-1",
  target: "application",
  cols: 80,
  rows: 24,
  expires_at: "2026-10-21T00:00:00Z",
  websocket_path: "/api/v1/terminal/sessions/session-1/ws",
  subprotocol: "gotham.terminal.v1",
};

const createInput: CreateApplicationInput = {
  name: "shop",
  environment_id: "env-1",
  provider: "docker",
  repo: "",
  clone_url: "",
  source_type: "dockerfile",
  dockerfile_content: "FROM nginx:1",
  branch: "main",
  build_pack: "",
  image_ref: "",
  server_id: "srv-1",
  env: [],
  storage: [],
};

const domainInput: DomainInput = {
  domain: "shop.example.com",
  port: 80,
  protocol: "http",
};

interface CallCase {
  name: string;
  method: "get" | "post" | "put" | "delete";
  path: string;
  run: () => Promise<unknown>;
}

const applicationCalls: CallCase[] = [
  {
    name: "getApplication (detail page)",
    method: "get",
    path: `/applications/${appId}`,
    run: () => getApplication(appId),
  },
  {
    name: "updateApplication",
    method: "put",
    path: `/applications/${appId}`,
    run: () => updateApplication(appId, { name: "renamed" }),
  },
  {
    name: "deleteApplication",
    method: "delete",
    path: `/applications/${appId}`,
    run: () => deleteApplication(appId),
  },
  {
    name: "createApplication",
    method: "post",
    path: "/applications",
    run: () => createApplication(createInput),
  },
  {
    name: "listRecentDeployments (dashboard feed)",
    method: "get",
    path: "/deployments",
    run: () => listRecentDeployments(5),
  },
  {
    name: "listDeployments (Deployments tab)",
    method: "get",
    path: `/applications/${appId}/deployments`,
    run: () => listDeployments(appId, 1),
  },
  {
    name: "getDeploymentBuildLog (Logs tab)",
    method: "get",
    path: `/applications/${appId}/deployments/dep-1/logs`,
    run: () => getDeploymentBuildLog(appId, "dep-1"),
  },
  {
    name: "getEnv (Environment tab)",
    method: "get",
    path: `/applications/${appId}/env`,
    run: () => getEnv(appId),
  },
  {
    name: "replaceEnv (Environment save)",
    method: "put",
    path: `/applications/${appId}/env`,
    run: () => replaceEnv(appId, []),
  },
  {
    name: "getStorages (Storage tab)",
    method: "get",
    path: `/applications/${appId}/storages`,
    run: () => getStorages(appId),
  },
  {
    name: "replaceStorages (Storage save)",
    method: "put",
    path: `/applications/${appId}/storages`,
    run: () => replaceStorages(appId, []),
  },
  {
    name: "listDomains (Domains tab)",
    method: "get",
    path: `/applications/${appId}/domains`,
    run: () => listDomains(appId),
  },
  {
    name: "addDomain",
    method: "post",
    path: `/applications/${appId}/domains`,
    run: () => addDomain(appId, domainInput),
  },
  {
    name: "updateDomain",
    method: "put",
    path: `/applications/${appId}/domains/dom-1`,
    run: () => updateDomain(appId, "dom-1", domainInput),
  },
  {
    name: "removeDomain",
    method: "delete",
    path: `/applications/${appId}/domains/dom-1`,
    run: () => removeDomain(appId, "dom-1"),
  },
  {
    name: "setPrimaryDomain",
    method: "post",
    path: `/applications/${appId}/domains/dom-1/primary`,
    run: () => setPrimaryDomain(appId, "dom-1"),
  },
  {
    name: "listPreviews (Previews tab)",
    method: "get",
    path: `/applications/${appId}/previews`,
    run: () => listPreviews(appId),
  },
  {
    name: "triggerDeploy",
    method: "post",
    path: `/applications/${appId}/deploy`,
    run: () => triggerDeploy(appId),
  },
  {
    name: "rollbackDeployment",
    method: "post",
    path: `/applications/${appId}/rollback`,
    run: () => rollbackDeployment(appId),
  },
  {
    name: "stopApplication",
    method: "post",
    path: `/applications/${appId}/stop`,
    run: () => stopApplication(appId),
  },
  {
    name: "startApplication",
    method: "post",
    path: `/applications/${appId}/start`,
    run: () => startApplication(appId),
  },
  {
    name: "getDeployKey",
    method: "get",
    path: `/applications/${appId}/deploy-key`,
    run: () => getDeployKey(appId),
  },
  {
    name: "createDeployKey",
    method: "post",
    path: `/applications/${appId}/deploy-key`,
    run: () => createDeployKey(appId),
  },
  {
    name: "deleteDeployKey",
    method: "delete",
    path: `/applications/${appId}/deploy-key`,
    run: () => deleteDeployKey(appId),
  },
  {
    name: "getGitCredential",
    method: "get",
    path: `/applications/${appId}/git-credential`,
    run: () => getGitCredential(appId),
  },
  {
    name: "setGitCredential",
    method: "put",
    path: `/applications/${appId}/git-credential`,
    run: () => setGitCredential(appId, "user", "token"),
  },
  {
    name: "deleteGitCredential",
    method: "delete",
    path: `/applications/${appId}/git-credential`,
    run: () => deleteGitCredential(appId),
  },
  {
    name: "testConnection",
    method: "post",
    path: `/applications/${appId}/test-connection`,
    run: () => testConnection(appId),
  },
  {
    name: "listApplicationTerminalTargets (Terminal tab discovery)",
    method: "get",
    path: `/applications/${appId}/terminal/targets`,
    run: () => listApplicationTerminalTargets(appId),
  },
  {
    name: "createApplicationTerminalSession (default team)",
    method: "post",
    path: `/applications/${appId}/terminal/sessions`,
    run: () =>
      createApplicationTerminalSession(appId, {
        service: "web",
        containerId: "cid-1",
        cols: 80,
        rows: 24,
      }),
  },
  {
    name: "closeApplicationTerminalSession (default team)",
    method: "delete",
    path: `/applications/${appId}/terminal/sessions/session-1`,
    run: () => closeApplicationTerminalSession(appId, "session-1"),
  },
];

function teamHeaderOf(
  config: InternalAxiosRequestConfig | undefined,
): string | null {
  return (config?.headers.get("X-Team-Id") as string | null | undefined) ?? null;
}

beforeEach(() => {
  captured = [];
  window.localStorage.setItem(activeTeamStorageKey, teamId);
  http.defaults.adapter = (config) => {
    captured.push(config);
    return Promise.resolve({
      data: payload,
      status: 200,
      statusText: "OK",
      headers: {},
      config,
    } as AxiosResponse);
  };
});

afterEach(() => {
  http.defaults.adapter = originalAdapter;
  window.localStorage.clear();
  captured = [];
});

describe("application-scoped requests send the active team", () => {
  for (const call of applicationCalls) {
    it(`${call.name} addresses ${call.path} with X-Team-Id`, async () => {
      await call.run();
      expect(captured).toHaveLength(1);
      expect(captured[0]!.method?.toUpperCase()).toBe(call.method.toUpperCase());
      expect(captured[0]!.url).toBe(call.path);
      expect(teamHeaderOf(captured[0])).toBe(teamId);
    });
  }

  it("an explicit team always wins over the active selection", async () => {
    await listApplications("team-explicit");
    expect(teamHeaderOf(captured[0])).toBe("team-explicit");
  });

  it("listApplications without a team follows the active selection", async () => {
    await listApplications();
    expect(teamHeaderOf(captured[0])).toBe(teamId);
  });

  it("an explicit team on a terminal session wins over the active selection", async () => {
    await createApplicationTerminalSession(
      appId,
      { service: "web", containerId: "cid-1", cols: 80, rows: 24 },
      undefined,
      "team-explicit",
    );
    expect(teamHeaderOf(captured[0])).toBe("team-explicit");
  });

  it("sends no team header when the personal team is active", async () => {
    window.localStorage.removeItem(activeTeamStorageKey);
    await getApplication(appId);
    expect(teamHeaderOf(captured[0])).toBeNull();
  });

  it("leaves non-application routes untouched", async () => {
    await http.get("/teams");
    expect(teamHeaderOf(captured[0])).toBeNull();
  });
});
