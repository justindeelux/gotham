
function escapeHtml(source: string): string {
  return source
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

export function isSafeLink(url: string): boolean {
  const trimmed = url.trim();
  if (!/^https?:\/\//i.test(trimmed)) {
    return false;
  }
  try {
    const parsed = new URL(trimmed);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

export function repoBaseFromReleaseUrl(releaseUrl: string): string {
  try {
    const parsed = new URL(releaseUrl);
    if (parsed.protocol !== "https:") {
      return "";
    }
    const parts = parsed.pathname.split("/").filter(Boolean);
    if (parts.length < 2) {
      return "";
    }
    return `${parsed.origin}/${parts[0]}/${parts[1]}`;
  } catch {
    return "";
  }
}

function linkAttrs(href: string): string {
  return `<a href="${href}" target="_blank" rel="noopener noreferrer">`;
}

function renderInline(escaped: string, repoBase: string): string {
  const codes: string[] = [];
  let out = escaped.replace(/&#39;|&quot;|`([^`]+)`/g, (match, code: string | undefined) => {
    if (code === undefined) {
      return match;
    }
    codes.push(`<code>${code}</code>`);
    return `\uE000${codes.length - 1}\uE001`;
  });
  out = out.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
  out = out.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_match, text: string, url: string) => {
    const raw = url.replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&amp;/g, "&");
    if (!isSafeLink(raw)) {
      return text;
    }
    return `${linkAttrs(escapeHtml(raw))}${text}</a>`;
  });
  if (repoBase) {
    let inLink = false;
    out = out
      .split(/(<[^>]*>)/)
      .map((part) => {
        if (part.startsWith("<")) {
          if (/^<a\s/i.test(part)) {
            inLink = true;
          } else if (/^<\/a>/i.test(part)) {
            inLink = false;
          }
          return part;
        }
        if (inLink) {
          return part;
        }
        return part.replace(/(^|\s)#(\d{1,6})\b/g, (_match, prefix: string, num: string) => {
          return `${prefix}${linkAttrs(`${repoBase}/issues/${num}`)}#${num}</a>`;
        });
      })
      .join("");
  }
  out = out.replace(/\uE000(\d+)\uE001/g, (_m, index: string) => codes[Number(index)] ?? "");
  return out;
}

export function renderMarkdown(source: string, releaseUrl = ""): string {
  const repoBase = releaseUrl ? repoBaseFromReleaseUrl(releaseUrl) : "";
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  const html: string[] = [];
  let list: "ul" | "ol" | null = null;
  let fence = false;

  function closeList(): void {
    if (list) {
      html.push(list === "ul" ? "</ul>" : "</ol>");
      list = null;
    }
  }

  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed.startsWith("```")) {
      if (fence) {
        html.push("</code></pre>");
      } else {
        closeList();
        html.push("<pre><code>");
      }
      fence = !fence;
      continue;
    }
    if (fence) {
      html.push(`${escapeHtml(line)}\n`);
      continue;
    }
    const heading = /^(#{1,3})\s+(.*)$/.exec(trimmed);
    if (heading) {
      closeList();
      const level = heading[1].length + 3;
      html.push(`<h${level}>${renderInline(escapeHtml(heading[2]), repoBase)}</h${level}>`);
      continue;
    }
    const bullet = /^[-*+]\s+(.*)$/.exec(trimmed);
    const ordered = /^\d+[.)]\s+(.*)$/.exec(trimmed);
    if (bullet || ordered) {
      const kind = bullet ? "ul" : "ol";
      if (list !== kind) {
        closeList();
        html.push(kind === "ul" ? "<ul>" : "<ol>");
        list = kind;
      }
      html.push(`<li>${renderInline(escapeHtml((bullet ?? ordered)?.[1] ?? ""), repoBase)}</li>`);
      continue;
    }
    if (!trimmed) {
      closeList();
      continue;
    }
    closeList();
    html.push(`<p>${renderInline(escapeHtml(trimmed), repoBase)}</p>`);
  }
  closeList();
  if (fence) {
    html.push("</code></pre>");
  }
  return html.join("");
}
