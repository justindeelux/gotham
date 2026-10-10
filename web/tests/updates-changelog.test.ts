import { describe, expect, it } from "vitest";

import {
  isSafeLink,
  renderMarkdown,
  repoBaseFromReleaseUrl,
} from "@/features/updates/markdown";
import { changelogSchema } from "@/features/updates/schemas/updates";
import en from "@/features/updates/locales/en";
import vi from "@/features/updates/locales/vi";

describe("updates markdown renderer", () => {
  it("renders headings, lists, code and links", () => {
    const html = renderMarkdown(
      "## Highlights\n- [PR #1](https://example.com/x)\n- run `gotham update`",
    );
    expect(html).toContain("<h5>Highlights</h5>");
    expect(html).toContain("<ul>");
    expect(html).toContain('target="_blank" rel="noopener noreferrer"');
    expect(html).toContain("<code>gotham update</code>");
  });

  it("neutralizes script injection and javascript: links", () => {
    const html = renderMarkdown(
      '<script>alert(1)</script>\n[click](javascript:alert(1))\n[ok](https://example.com/)',
      "https://github.com/o/r/releases/tag/v1.0.0",
    );
    expect(html).not.toContain("<script>");
    expect(html).toContain("&lt;script&gt;");
    expect(html).not.toContain("javascript:");
    expect(html).toContain('href="https://example.com/"');
  });

  it("drops non-http links to plain text", () => {
    expect(isSafeLink("https://github.com/o/r")).toBe(true);
    expect(isSafeLink("http://mirror.local/r")).toBe(true);
    expect(isSafeLink("javascript:alert(1)")).toBe(false);
    expect(isSafeLink("/relative/path")).toBe(false);
    expect(isSafeLink("data:text/html,hi")).toBe(false);
    const html = renderMarkdown("[x](javascript:alert(1))");
    expect(html).not.toContain("<a ");
    expect(html).toContain("x");
  });

  it("links #123 issue references to the release repo", () => {
    expect(repoBaseFromReleaseUrl("https://github.com/o/r/releases/tag/v1.0.0")).toBe(
      "https://github.com/o/r",
    );
    expect(repoBaseFromReleaseUrl("javascript:alert(1)")).toBe("");
    const html = renderMarkdown("Fixed by #42", "https://github.com/o/r/releases/tag/v1.0.0");
    expect(html).toContain('href="https://github.com/o/r/issues/42"');
  });

  it("renders fenced code blocks without interpreting markup", () => {
    const html = renderMarkdown("```\n<script>x</script>\n```");
    expect(html).toContain("<pre><code>");
    expect(html).toContain("&lt;script&gt;");
  });
});

describe("updates changelog schema", () => {
  it("parses the changelog envelope and bounds-agnostic entries", () => {
    const parsed = changelogSchema.safeParse({
      current: "v1.0.0",
      entries: [
        {
          version: "v1.2.0",
          channel: "stable",
          notes: "## New",
          html_url: "https://github.com/o/r/releases/tag/v1.2.0",
          published_at: "2026-01-02T15:04:05Z",
        },
      ],
    });
    expect(parsed.success).toBe(true);
  });

  it("accepts the dev empty state", () => {
    expect(changelogSchema.safeParse({ current: "0.2.x-dev", entries: [] }).success).toBe(true);
  });

  it("rejects entries without a version", () => {
    expect(changelogSchema.safeParse({ current: "v1.0.0", entries: [{}] }).success).toBe(false);
  });
});

describe("updates changelog locales", () => {
  it("never links issue refs inside a tag or an existing link", () => {
    const release = "https://github.com/o/r/releases/tag/v1.0.0";
    const inHref = renderMarkdown("[t](https://e.com/ #1)", release);
    expect(inHref).toBe(
      '<p><a href="https://e.com/ #1" target="_blank" rel="noopener noreferrer">t</a></p>',
    );
    const inText = renderMarkdown("[fix #2](https://e.com/x)", release);
    expect(inText.match(/<a /g)).toHaveLength(1);
  });

  it("keeps en/vi keys in sync", () => {
    expect(Object.keys(en.changelog).sort()).toEqual(Object.keys(vi.changelog).sort());
    for (const key of Object.keys(en.changelog) as (keyof typeof en.changelog)[]) {
      expect(typeof vi.changelog[key]).toBe("string");
      expect(vi.changelog[key].length).toBeGreaterThan(0);
    }
  });
});
