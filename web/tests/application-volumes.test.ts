// JUS-273: the Storage tab lists an application's volumes for every source.
// The fixtures here are byte-for-byte the shape internal/deploy emits
// (volumeListEnvelope, optional fields omitted), and the response schema is
// validated strictly so a UI fake cannot accept a shape the backend never
// sends.

import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";

import ApplicationStorageTab from "@/features/applications/components/ApplicationStorageTab.vue";
import StorageEditor from "@/features/applications/components/StorageEditor.vue";
import { volumeListingSchema, volumeSchema } from "@/features/applications/schemas/volumes";
import type { VolumeListing } from "@/features/applications/schemas/volumes";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";
import { parseWith } from "@/shared/validation/parse";

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

/** composeListing is one managed volume shared by two compose services. */
function composeListing(): VolumeListing {
  return {
    source_type: "compose",
    runtime: "available",
    volumes: [
      {
        name: "shared",
        kind: "managed",
        host_path: "/var/lib/gotham/volumes/app-1/shared",
        state: "running",
        mounts: [
          {
            destination: "/var/www/html",
            read_only: false,
            service: "web",
            container: "shop-web-1",
            state: "running",
          },
          {
            destination: "/work",
            read_only: true,
            service: "worker",
            container: "shop-worker-1",
            state: "stopped",
          },
        ],
        file_manager: { server_id: "server-1", root: "volumes", path: "app-1/shared" },
      },
    ],
  };
}

/** fullAccessListing is the backend shape on a default agent (single "system"
 * root at "/"): the path carries the managed directory without its leading
 * slash. */
function fullAccessListing(): VolumeListing {
  return {
    source_type: "image",
    runtime: "available",
    volumes: [
      {
        name: "web_data",
        kind: "managed",
        host_path: "/var/lib/gotham/volumes/app-1/web_data",
        state: "running",
        mounts: [{ destination: "/data", read_only: false, state: "running" }],
        file_manager: {
          server_id: "server-1",
          root: "system",
          path: "var/lib/gotham/volumes/app-1/web_data",
        },
      },
    ],
  };
}

/** namedListing is a non-compose application whose named volume is not in the
 * File Manager roots; the backend sends a reason and no target. */
function namedListing(): VolumeListing {
  return {
    source_type: "dockerfile",
    runtime: "available",
    volumes: [
      {
        name: "cache",
        kind: "named",
        host_path: "/var/lib/docker/volumes/gotham-app-1-cache/_data",
        state: "running",
        mounts: [
          {
            destination: "/cache",
            read_only: true,
            state: "running",
          },
        ],
        open_reason: "not_browsable",
      },
    ],
  };
}

/** mountTab renders the Storage tab with the props the page passes. */
function mountTab(listing: VolumeListing | null, sourceType = listing?.source_type ?? "git_public") {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/servers/:id/files", name: "server-files", component: { template: "<div />" } },
      { path: "/", component: { template: "<div />" } },
    ],
  });
  return mount(ApplicationStorageTab, {
    props: {
      serverId: "server-1",
      sourceType,
      volumes: listing,
      volumesLoading: false,
      volumesError: null,
      storagesDraft: [],
      storagesLoading: false,
      storagesError: null,
      saveDisabled: false,
      saving: false,
    },
    global: { plugins: [router, i18n] },
  });
}

/** findOpenButton returns the File Manager action button of a volume row. */
function findOpenButton(wrapper: ReturnType<typeof mountTab>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().includes("Open in File Manager"));
}

describe("application volume listing schema", () => {
  it("pins every optional field the tab reads", () => {
    // A rename of host_path/file_manager/open_reason must fail a test, not
    // silently degrade to an absent optional.
    expect(Object.keys(volumeSchema.shape)).toEqual([
      "name",
      "kind",
      "host_path",
      "state",
      "mounts",
      "file_manager",
      "open_reason",
    ]);
  });

  it("accepts the backend shape with optional fields omitted", () => {
    expect(() =>
      parseWith(volumeListingSchema, composeListing(), { strict: true }),
    ).not.toThrow();
    expect(() =>
      parseWith(volumeListingSchema, namedListing(), { strict: true }),
    ).not.toThrow();
    // The minimal shape omits reason, host_path, file_manager and the mount's
    // service/container, exactly like the Go `omitempty` tags.
    expect(() =>
      parseWith(
        volumeListingSchema,
        {
          source_type: "git_public",
          runtime: "unavailable",
          volumes: [
            { name: "data", kind: "managed", state: "unverified", mounts: [] },
          ],
        },
        { strict: true },
      ),
    ).not.toThrow();
  });

  it("rejects a shape the backend never emits", () => {
    expect(() =>
      parseWith(
        volumeListingSchema,
        {
          source_type: "compose",
          runtime: "available",
          volumes: [{ name: "data", kind: "managed", mounts: [{ destination: "/data" }] }],
        },
        { strict: true },
      ),
    ).toThrow();
  });
});

describe("ApplicationStorageTab volume list", () => {
  it("shows both consumers of a shared compose volume and the managed directory", () => {
    const wrapper = mountTab(composeListing());
    const text = wrapper.text();
    expect(text).toContain("shared");
    expect(text).toContain("/var/lib/gotham/volumes/app-1/shared");
    expect(text).toContain("/var/www/html");
    expect(text).toContain("web");
    expect(text).toContain("/work");
    expect(text).toContain("worker");
    expect(text).toContain("read-only");
    expect(text).toContain("Running");
    expect(text).toContain("/var/lib/gotham/volumes/app-1/shared");
    const open = findOpenButton(wrapper);
    expect(open?.attributes("aria-label")).toBe(
      "Open volume shared in File Manager",
    );
    wrapper.unmount();
  });

  it("opens the validated File Manager target in the same browser tab", async () => {
    const wrapper = mountTab(composeListing());
    const open = findOpenButton(wrapper);
    expect(open, "the volume offers Open in File Manager").toBeTruthy();
    // A button, not a link: the SPA navigates in the current browser tab.
    expect(open?.attributes("href")).toBeUndefined();
    expect(open?.attributes("target")).toBeUndefined();
    expect(open?.attributes("aria-label")).toBe(
      "Open volume shared in File Manager",
    );

    await open?.trigger("click");
    await flushPromises();

    const route = wrapper.vm.$router.currentRoute.value;
    expect(route.name).toBe("server-files");
    expect(route.params.id).toBe("server-1");
    expect(route.query).toMatchObject({ root: "volumes", path: "app-1/shared" });
    wrapper.unmount();
  });

  it("shows an unknown server reason verbatim", () => {
    const listing = namedListing();
    listing.volumes[0].open_reason = "a newer server reason";
    const wrapper = mountTab(listing);
    expect(wrapper.text()).toContain("a newer server reason");
    wrapper.unmount();
  });

  it("translates the files-disabled reason code", () => {
    const listing = namedListing();
    listing.volumes[0].open_reason = "files_disabled";
    const wrapper = mountTab(listing);
    expect(wrapper.text()).toContain("File Manager is disabled on this server");
    wrapper.unmount();
  });

  it("opens the root the backend chose (full-access system mode)", async () => {
    const wrapper = mountTab(fullAccessListing());
    const open = findOpenButton(wrapper);
    expect(open).toBeTruthy();

    await open?.trigger("click");
    await flushPromises();

    const route = wrapper.vm.$router.currentRoute.value;
    expect(route.name).toBe("server-files");
    expect(route.query).toMatchObject({
      root: "system",
      path: "var/lib/gotham/volumes/app-1/web_data",
    });
    wrapper.unmount();
  });

  it("refuses a target that names another server", () => {
    const listing = composeListing();
    listing.volumes[0].file_manager = {
      server_id: "server-2",
      root: "volumes",
      path: "app-1/shared",
    };
    const wrapper = mountTab(listing);
    expect(findOpenButton(wrapper)).toBeUndefined();
    expect(wrapper.text()).toContain(
      "That directory is not part of this application's storage",
    );
    wrapper.unmount();
  });

  it("has en and vi copy for every backend open_reason code", () => {
    const codes = [
      "files_disabled",
      "root_unavailable",
      "not_browsable",
      "volume_unverified",
      "no_node",
    ];
    for (const code of codes) {
      for (const locale of ["en", "vi"] as const) {
        syncComposerLocale(locale);
        const listing = namedListing();
        listing.volumes[0].open_reason = code;
        const wrapper = mountTab(listing);
        const text = wrapper.text();
        expect(text, `${locale} ${code}`).not.toContain(code);
        expect(text.length, `${locale} ${code}`).toBeGreaterThan(0);
        wrapper.unmount();
      }
    }
  });

  it("explains why a named volume has no directory action", () => {
    const wrapper = mountTab(namedListing());
    const text = wrapper.text();
    expect(text).toContain("not browsable in File Manager");
    expect(findOpenButton(wrapper)).toBeUndefined();
    wrapper.unmount();
  });

  it("keeps the mapping editor for non-compose sources and hides it for compose", () => {
    const compose = mountTab(composeListing());
    expect(compose.findComponent(StorageEditor).exists()).toBe(false);
    expect(compose.text()).toContain("Compose volumes");
    compose.unmount();

    const single = mountTab(namedListing());
    expect(single.findComponent(StorageEditor).exists()).toBe(true);
    single.unmount();
  });

  it("renders an empty state instead of a blank table", () => {
    const wrapper = mountTab({
      source_type: "git_public",
      runtime: "available",
      volumes: [],
    });
    expect(wrapper.text()).toContain("No volumes yet");
    wrapper.unmount();
  });

  it("surfaces a limited runtime with its reason", () => {
    const listing: VolumeListing = {
      source_type: "dockerfile",
      runtime: "limited",
      reason:
        "the node agent does not report mount types; update the agent to identify every mount",
      volumes: [],
    };
    const wrapper = mountTab(listing);
    expect(wrapper.text()).toContain("does not report mount types");
    wrapper.unmount();
  });
});
