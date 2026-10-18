// Editor autocomplete and snippets (JUS-227): per-language sources, the
// word-in-document fallback, the two settings switches, and the live
// compartment removal when autocomplete is turned off.
/* global document:readonly, KeyboardEvent:readonly, setTimeout:readonly */
import { completionStatus, startCompletion } from "@codemirror/autocomplete";
import { Compartment, EditorState } from "@codemirror/state";
import type { Extension } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { afterEach, describe, expect, it } from "vitest";

import { editorSettingsExtensions } from "@/features/files/editor/codemirror";
import {
  completionExtensions,
  completionSources,
  keywordCompletions,
  snippetCompletions,
  snippetSpecs,
} from "@/features/files/editor/completion";
import {
  DEFAULT_EDITOR_SETTINGS,
  EDITOR_SETTINGS_VERSION,
  parseEditorSettings,
} from "@/features/files/schemas/editorSettings";

/** wait leaves the autocomplete plugin time to resolve its sources. */
function wait(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** makeView attaches one editor view with the given extensions. */
function makeView(extensions: Extension, doc = "do"): EditorView {
  const host = document.createElement("div");
  document.body.append(host);
  return new EditorView({
    parent: host,
    state: EditorState.create({
      doc,
      selection: { anchor: doc.length },
      extensions,
    }),
  });
}

/** expandSnippet applies one snippet at the start of an empty document. */
function expandSnippet(language: "shell" | "dockerfile" | "yaml" | "nginx", label: string): string {
  const completion = snippetCompletions(language).find((item) => item.label === label);
  if (typeof completion?.apply !== "function") {
    throw new Error(`snippet not found: ${language}/${label}`);
  }
  const view = makeView([], "");
  completion.apply(view, completion, 0, 0);
  const doc = view.state.doc.toString();
  view.destroy();
  return doc;
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("editor settings with autocomplete", () => {
  it("defaults the new switches when reading preferences stored before JUS-227", () => {
    const { autocomplete, snippets, ...legacy } = DEFAULT_EDITOR_SETTINGS;
    expect(autocomplete).toBe(true);
    expect(snippets).toBe(true);

    const parsed = parseEditorSettings({
      version: EDITOR_SETTINGS_VERSION,
      ...legacy,
      theme: "light",
      tabSize: 2,
    });
    expect(parsed.theme).toBe("light");
    expect(parsed.tabSize).toBe(2);
    expect(parsed.autocomplete).toBe(true);
    expect(parsed.snippets).toBe(true);
  });

  it("resets only a bogus new switch", () => {
    const parsed = parseEditorSettings({
      version: EDITOR_SETTINGS_VERSION,
      ...DEFAULT_EDITOR_SETTINGS,
      autocomplete: "yes",
      snippets: null,
      wrap: true,
    });
    expect(parsed.autocomplete).toBe(true);
    expect(parsed.snippets).toBe(true);
    expect(parsed.wrap).toBe(true);
  });
});

describe("completion sources", () => {
  it("offers per-language keywords", () => {
    expect(keywordCompletions("dockerfile").map((item) => item.label)).toContain("FROM");
    expect(keywordCompletions("shell").map((item) => item.label)).toContain("systemctl");
    expect(keywordCompletions("plain")).toEqual([]);
  });

  it("ships snippets for shell, Dockerfile, YAML and nginx only", () => {
    for (const language of ["shell", "dockerfile", "yaml", "nginx"] as const) {
      expect(snippetCompletions(language).length).toBeGreaterThan(0);
      expect(snippetCompletions(language).every((item) => item.apply)).toBe(true);
    }
    expect(snippetCompletions("python")).toEqual([]);
  });

  it("never leaves an empty numeric field in a snippet", () => {
    for (const language of ["shell", "dockerfile", "yaml", "nginx"] as const) {
      for (const spec of snippetSpecs(language)) {
        const placeholders = [...spec.template.matchAll(/\$\{([^}]*)\}/g)];
        placeholders.forEach(([, body = ""], index) => {
          if (body === "") {
            // A bare `${}` is the final cursor stop and must come last.
            expect(index, `${language}/${spec.label}: empty field before the end`).toBe(
              placeholders.length - 1,
            );
            return;
          }
          // `${1}` is an empty tab stop; digits must carry a default.
          expect(body, `${language}/${spec.label}: ${body}`).not.toMatch(/^\d+$/);
        });
        expect(expandSnippet(language, spec.label)).not.toContain("${");
      }
    }
  });

  it("expands numeric placeholders with their real defaults", () => {
    expect(expandSnippet("yaml", "compose-service")).toContain('"8080:80"');
    expect(expandSnippet("yaml", "deployment")).toContain("replicas: 1");
    expect(expandSnippet("yaml", "service")).toContain("port: 80");
    expect(expandSnippet("yaml", "service")).toContain("targetPort: 8080");
    expect(expandSnippet("nginx", "server")).toContain("listen 80;");
    expect(expandSnippet("dockerfile", "multi-stage")).toContain(
      "FROM golang:1 AS build",
    );
  });

  it("expands a snippet template at the cursor", () => {
    const shebang = snippetCompletions("shell").find((item) => item.label === "shebang");
    expect(shebang?.apply).toBeTypeOf("function");
    const view = makeView([]);
    if (typeof shebang?.apply === "function") {
      shebang.apply(view, shebang, 0, 0);
    }
    expect(view.state.doc.toString()).toContain("#!/usr/bin/env bash");
    view.destroy();
  });

  it("adds the word-in-document fallback and follows both switches", () => {
    expect(completionSources(DEFAULT_EDITOR_SETTINGS, "plain")).toHaveLength(1);
    expect(completionSources(DEFAULT_EDITOR_SETTINGS, "yaml")).toHaveLength(3);
    expect(
      completionSources({ ...DEFAULT_EDITOR_SETTINGS, snippets: false }, "yaml"),
    ).toHaveLength(2);
    expect(
      completionSources({ ...DEFAULT_EDITOR_SETTINGS, autocomplete: false }, "yaml"),
    ).toEqual([]);
  });

  it("fully removes the extension when autocomplete is off", () => {
    expect(completionExtensions(DEFAULT_EDITOR_SETTINGS, "shell")).not.toEqual([]);
    expect(
      completionExtensions({ ...DEFAULT_EDITOR_SETTINGS, autocomplete: false }, "shell"),
    ).toEqual([]);
  });
});

describe("live completion reconfiguration", () => {
  it("keeps bracket closing when autocomplete is off", () => {
    const settings = { ...DEFAULT_EDITOR_SETTINGS, autocomplete: false };
    const view = makeView(editorSettingsExtensions(settings, "plain"), "x");
    const from = view.state.selection.main.from;
    const handled = view.state
      .facet(EditorView.inputHandler)
      .some((handler) =>
        (
          handler as unknown as (
            _view: EditorView,
            _from: number,
            _to: number,
            _text: string,
          ) => boolean
        )(view, from, from, "("),
      );

    expect(handled).toBe(true);
    expect(view.state.doc.toString()).toBe("x()");

    view.contentDOM.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Backspace",
        code: "Backspace",
        bubbles: true,
        cancelable: true,
      }),
    );
    expect(view.state.doc.toString()).toBe("x");
    view.destroy();
  });

  it("accepts the selected option with Tab and leaves Tab alone when closed", async () => {
    const view = makeView(
      completionExtensions(DEFAULT_EDITOR_SETTINGS, "shell"),
      "docke",
    );
    expect(startCompletion(view)).toBe(true);
    await wait(250);

    const accept = new KeyboardEvent("keydown", {
      key: "Tab",
      code: "Tab",
      bubbles: true,
      cancelable: true,
    });
    view.contentDOM.dispatchEvent(accept);
    expect(view.state.doc.toString()).toBe("docker");
    expect(accept.defaultPrevented).toBe(true);

    const fallthrough = new KeyboardEvent("keydown", {
      key: "Tab",
      code: "Tab",
      bubbles: true,
      cancelable: true,
    });
    view.contentDOM.dispatchEvent(fallthrough);
    expect(fallthrough.defaultPrevented).toBe(false);
    view.destroy();
  });

  it("opens a popup and removes it when autocomplete is disabled", async () => {
    const compartment = new Compartment();
    const view = makeView([
      compartment.of(editorSettingsExtensions(DEFAULT_EDITOR_SETTINGS, "shell")),
    ]);

    expect(startCompletion(view)).toBe(true);
    await wait(250);
    expect(completionStatus(view.state)).toBe("active");
    expect(view.dom.querySelector(".cm-tooltip-autocomplete")).not.toBeNull();

    view.dispatch({
      effects: compartment.reconfigure(
        editorSettingsExtensions(
          { ...DEFAULT_EDITOR_SETTINGS, autocomplete: false },
          "shell",
        ),
      ),
    });

    expect(startCompletion(view)).toBe(false);
    await wait(20);
    expect(completionStatus(view.state)).toBeNull();
    expect(view.dom.querySelector(".cm-tooltip-autocomplete")).toBeNull();
    view.destroy();
  });
});
