// Files search results (JUS-163 fix round 1): the polite live region announces
// the searching/count/empty outcome, the truncated note is visible, and a row
// click / Clear emit the right events. The shared i18n plugin supplies the
// files catalog so the count plural and copy resolve.
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";

import FilesSearchResults from "@/features/files/components/FilesSearchResults.vue";
import type {
  FilesSearchHit,
  FileKind,
} from "@/features/files/schemas/files";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

/** hit builds one search result with sensible defaults. */
function hit(path: string, name: string, kind: FileKind = "file"): FilesSearchHit {
  return {
    path,
    entry: {
      name,
      kind,
      size: 0,
      mode: kind === "directory" ? 0o040755 : 0o100644,
      modified_at: "2026-10-12T09:02:00Z",
      etag: '"1"',
    },
  };
}

function mountResults(props: {
  results: FilesSearchHit[];
  truncated: boolean;
  loading: boolean;
  query: string;
  path: string;
}) {
  return mount(FilesSearchResults, {
    props,
    global: { plugins: [i18n] },
  });
}

beforeEach(() => {
  resetLocaleState();
  registerDiscoveredCatalogs();
  syncComposerLocale("en");
});

describe("files search results", () => {
  it("announces the search is running while it loads", () => {
    const wrapper = mountResults({
      results: [],
      truncated: false,
      loading: true,
      query: "conf",
      path: "app",
    });
    const live = wrapper.find(".fm-search-live");
    expect(live.attributes("role")).toBe("status");
    expect(live.attributes("aria-live")).toBe("polite");
    expect(live.text()).toContain("Searching");
  });

  it("announces the plural result count and lists the hits", () => {
    const wrapper = mountResults({
      results: [
        hit("app/config.yaml", "config.yaml"),
        hit("app/sub/config.json", "config.json"),
      ],
      truncated: false,
      loading: false,
      query: "config",
      path: "",
    });
    expect(wrapper.find(".fm-search-live").text()).toContain("2 results");
    const rows = wrapper.findAll(".fm-search-row");
    expect(rows).toHaveLength(2);
    expect(rows[0].text()).toContain("config.yaml");
    expect(rows[0].text()).toContain("app");
  });

  it("uses the singular count for one hit", () => {
    const wrapper = mountResults({
      results: [hit("config.yaml", "config.yaml")],
      truncated: false,
      loading: false,
      query: "config",
      path: "",
    });
    expect(wrapper.find(".fm-search-live").text()).toContain("1 result");
  });

  it("announces the empty state when nothing matches", () => {
    const wrapper = mountResults({
      results: [],
      truncated: false,
      loading: false,
      query: "nope",
      path: "",
    });
    expect(wrapper.find(".fm-search-live").text()).toContain("No matching names");
  });

  it("appends the truncated note to the announcement", () => {
    const wrapper = mountResults({
      results: [hit("a.txt", "a.txt")],
      truncated: true,
      loading: false,
      query: "a",
      path: "",
    });
    expect(wrapper.find(".fm-search-live").text()).toContain("refine the search");
    expect(wrapper.find(".fm-search-truncated").exists()).toBe(true);
  });

  it("single click only selects a row, it never opens it", async () => {
    const target = hit("app/config.yaml", "config.yaml");
    const wrapper = mountResults({
      results: [target],
      truncated: false,
      loading: false,
      query: "config",
      path: "",
    });
    const row = wrapper.find(".fm-search-row");
    await row.trigger("click");
    expect(wrapper.emitted("open")).toBeUndefined();
    expect(row.classes()).toContain("is-selected");
  });

  it("double click opens the hit exactly once", async () => {
    const target = hit("app/config.yaml", "config.yaml");
    const wrapper = mountResults({
      results: [target],
      truncated: false,
      loading: false,
      query: "config",
      path: "",
    });
    const row = wrapper.find(".fm-search-row");
    // A real double-click is two clicks and then a dblclick. Only the dblclick
    // opens, so the second click can never land on a new listing (JUS-163 round 3).
    await row.trigger("click");
    await row.trigger("click");
    await row.trigger("dblclick");
    const opens = wrapper.emitted("open");
    expect(opens).toHaveLength(1);
    expect(opens?.[0]).toEqual([target]);
  });

  it("Enter and Space open the focused row", async () => {
    const target = hit("app/config.yaml", "config.yaml");
    const wrapper = mountResults({
      results: [target],
      truncated: false,
      loading: false,
      query: "config",
      path: "",
    });
    const row = wrapper.find(".fm-search-row");
    await row.trigger("keydown", { key: "Enter" });
    await row.trigger("keydown", { key: " " });
    expect(wrapper.emitted("open")).toEqual([[target], [target]]);
  });

  it("emits clear on the Clear button", async () => {
    const wrapper = mountResults({
      results: [hit("app/config.yaml", "config.yaml")],
      truncated: false,
      loading: false,
      query: "config",
      path: "",
    });
    const clear = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Clear search"));
    expect(clear).toBeTruthy();
    await clear?.trigger("click");
    expect(wrapper.emitted("clear")).toHaveLength(1);
  });
});
