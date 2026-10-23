import {
  acceptCompletion,
  autocompletion,
  closeBrackets,
  closeBracketsKeymap,
  completeAnyWord,
  completeFromList,
  completionStatus,
  snippetCompletion,
} from "@codemirror/autocomplete";
import type { Completion, CompletionSource } from "@codemirror/autocomplete";
import type { Extension } from "@codemirror/state";
import { keymap } from "@codemirror/view";

import type { EditorSettings } from "@/features/files/schemas/editorSettings";
import type { EditorLanguageId } from "@/features/files/utils/editor";

export interface SnippetSpec {
  label: string;
  template: string;
}

const KEYWORDS: Partial<Record<EditorLanguageId, readonly string[]>> = {
  json: ["true", "false", "null"],
  yaml: [
    "apiVersion", "kind", "metadata", "spec", "name", "namespace", "labels",
    "selector", "template", "containers", "image", "command", "entrypoint",
    "ports", "volumes", "environment", "depends_on", "restart", "networks",
    "healthcheck", "replicas", "service", "ingress", "true", "false", "null",
  ],
  javascript: [
    "const", "let", "var", "function", "return", "if", "else", "for", "while",
    "do", "switch", "case", "break", "continue", "class", "extends", "new",
    "this", "super", "import", "from", "export", "default", "async", "await",
    "try", "catch", "finally", "throw", "typeof", "instanceof", "in", "of",
    "delete", "void", "yield", "null", "undefined", "true", "false", "console",
    "document", "window", "JSON", "Promise", "Math", "Object", "Array", "String",
    "Number", "Boolean",
  ],
  go: [
    "package", "import", "func", "var", "const", "type", "struct", "interface",
    "map", "chan", "go", "defer", "return", "if", "else", "for", "range",
    "switch", "case", "default", "break", "continue", "select", "nil", "true",
    "false", "string", "int", "int64", "float64", "bool", "byte", "rune",
    "error", "make", "new", "len", "cap", "append", "copy", "delete", "panic",
    "recover",
  ],
  python: [
    "def", "class", "return", "if", "elif", "else", "for", "while", "break",
    "continue", "import", "from", "as", "pass", "raise", "try", "except",
    "finally", "with", "lambda", "yield", "global", "nonlocal", "assert", "del",
    "in", "is", "not", "and", "or", "None", "True", "False", "async", "await",
  ],
  java: [
    "class", "interface", "enum", "record", "extends", "implements", "package",
    "import", "public", "private", "protected", "static", "final", "abstract",
    "void", "int", "long", "double", "float", "boolean", "char", "String",
    "new", "return", "if", "else", "for", "while", "do", "switch", "case",
    "break", "continue", "try", "catch", "finally", "throw", "throws", "this",
    "super", "null", "true", "false",
  ],
  rust: [
    "fn", "let", "mut", "const", "static", "struct", "enum", "trait", "impl",
    "pub", "use", "mod", "crate", "self", "super", "match", "if", "else",
    "loop", "while", "for", "in", "return", "break", "continue", "as", "where",
    "async", "await", "move", "ref", "type", "unsafe", "dyn", "Some", "None",
    "Ok", "Err", "true", "false",
  ],
  cpp: [
    "class", "struct", "namespace", "template", "typename", "using", "include",
    "define", "if", "else", "for", "while", "do", "switch", "case", "break",
    "continue", "return", "const", "constexpr", "static", "inline", "virtual",
    "public", "private", "protected", "new", "delete", "nullptr", "true",
    "false", "int", "long", "double", "float", "bool", "char", "void", "auto",
  ],
  php: [
    "<?php", "function", "class", "interface", "trait", "extends", "implements",
    "public", "private", "protected", "static", "const", "new", "return", "if",
    "else", "elseif", "foreach", "for", "while", "do", "switch", "case",
    "break", "continue", "try", "catch", "finally", "throw", "namespace", "use",
    "echo", "print", "null", "true", "false", "array",
  ],
  ruby: [
    "def", "class", "module", "end", "if", "elsif", "else", "unless", "while",
    "until", "for", "in", "do", "begin", "rescue", "ensure", "raise", "return",
    "yield", "require", "require_relative", "attr_reader", "attr_writer",
    "attr_accessor", "nil", "true", "false", "self", "super",
  ],
  lua: [
    "function", "local", "return", "if", "then", "elseif", "else", "end", "for",
    "while", "repeat", "until", "do", "break", "in", "pairs", "ipairs", "nil",
    "true", "false", "and", "or", "not", "require", "print",
  ],
  sql: [
    "SELECT", "FROM", "WHERE", "JOIN", "LEFT", "RIGHT", "INNER", "OUTER", "ON",
    "GROUP", "ORDER", "BY", "HAVING", "LIMIT", "OFFSET", "INSERT", "INTO",
    "VALUES", "UPDATE", "SET", "DELETE", "CREATE", "ALTER", "DROP", "TABLE",
    "INDEX", "VIEW", "AND", "OR", "NOT", "NULL", "DISTINCT", "COUNT", "AS",
  ],
  shell: [
    "if", "then", "elif", "else", "fi", "for", "while", "until", "do", "done",
    "case", "esac", "function", "in", "local", "export", "readonly", "return",
    "exit", "sudo", "systemctl", "docker", "kubectl", "git", "curl", "wget",
    "grep", "sed", "awk", "chmod", "chown", "mkdir", "rm", "cp", "mv", "ln",
    "tar", "ssh", "scp", "rsync", "echo",
  ],
  dockerfile: [
    "FROM", "RUN", "CMD", "LABEL", "MAINTAINER", "EXPOSE", "ENV", "ADD", "COPY",
    "ENTRYPOINT", "VOLUME", "USER", "WORKDIR", "ARG", "ONBUILD", "STOPSIGNAL",
    "HEALTHCHECK", "SHELL",
  ],
  nginx: [
    "server", "location", "listen", "server_name", "root", "index", "proxy_pass",
    "proxy_set_header", "upstream", "include", "ssl_certificate",
    "ssl_certificate_key", "return", "rewrite", "try_files", "access_log",
    "error_log", "client_max_body_size", "gzip", "add_header", "deny", "allow",
    "auth_basic", "fastcgi_pass", "proxy_http_version", "expires",
  ],
  apache: [
    "ServerName", "ServerAlias", "DocumentRoot", "Listen", "LoadModule",
    "RewriteEngine", "RewriteRule", "RewriteCond", "ProxyPass", "ProxyPassReverse",
    "Directory", "DirectoryIndex", "AllowOverride", "Require", "ErrorLog",
    "CustomLog", "SSLEngine", "SSLCertificateFile", "SSLCertificateKeyFile",
    "Header", "Options",
  ],
  makefile: [
    ".PHONY", "include", "export", "override", "define", "endef", "ifeq",
    "ifneq", "ifdef", "ifndef", "else", "endif", "wildcard", "patsubst",
    "subst", "shell",
  ],
  toml: ["true", "false"],
  html: [
    "div", "span", "a", "p", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol",
    "li", "img", "table", "thead", "tbody", "tr", "th", "td", "form", "label",
    "input", "button", "select", "option", "textarea", "section", "header",
    "footer", "nav", "main", "aside", "script", "style", "link", "meta", "br",
    "hr", "pre", "code", "strong", "em",
  ],
  css: [
    "display", "position", "top", "right", "bottom", "left", "width", "height",
    "min-width", "max-width", "min-height", "max-height", "margin", "padding",
    "gap", "flex", "flex-direction", "flex-wrap", "align-items",
    "justify-content", "grid", "grid-template-columns", "grid-template-rows",
    "grid-column", "grid-row", "color", "background", "background-color",
    "background-image", "border", "border-radius", "box-shadow", "font",
    "font-family", "font-size", "font-weight", "line-height", "text-align",
    "text-decoration", "overflow", "opacity", "transform", "transition", "z-index",
  ],
};

const SNIPPETS: Partial<Record<EditorLanguageId, readonly SnippetSpec[]>> = {
  shell: [
    { label: "shebang", template: "#!/usr/bin/env bash\nset -euo pipefail\n${}" },
    { label: "if", template: "if [ ${condition} ]; then\n  ${}\nfi" },
    { label: "for", template: "for ${item} in ${items}; do\n  ${}\ndone" },
    { label: "while", template: "while ${condition}; do\n  ${}\ndone" },
    {
      label: "case",
      template:
        'case "${value}" in\n  ${pattern})\n    ${}\n    ;;\n  *)\n    ;;\nesac',
    },
    { label: "function", template: "${name}() {\n  ${}\n}" },
    { label: "heredoc", template: "cat <<'EOF'\n${}\nEOF" },
    { label: "script", template: "#!/usr/bin/env bash\nset -euo pipefail\n\n${}\n" },
  ],
  dockerfile: [
    { label: "FROM", template: "FROM ${image}:${tag}" },
    { label: "RUN", template: "RUN ${command}" },
    { label: "COPY", template: "COPY ${src} ${dest}" },
    { label: "WORKDIR", template: "WORKDIR ${path}" },
    { label: "ENV", template: "ENV ${key}=${value}" },
    { label: "EXPOSE", template: "EXPOSE ${port}" },
    { label: "ENTRYPOINT", template: 'ENTRYPOINT ["${executable}"]' },
    { label: "CMD", template: 'CMD ["${executable}"]' },
    {
      label: "HEALTHCHECK",
      template: "HEALTHCHECK --interval=30s --timeout=3s CMD ${command}",
    },
    {
      label: "multi-stage",
      template:
        "FROM ${1:golang:1} AS build\nWORKDIR /src\nCOPY . .\nRUN ${2:go build -o /app .}\n\nFROM ${3:alpine:3}\nCOPY --from=build /app /usr/local/bin/app\nENTRYPOINT [\"app\"]",
    },
  ],
  yaml: [
    {
      label: "compose-service",
      template:
        "services:\n  ${name}:\n    image: ${image}\n    restart: unless-stopped\n    ports:\n      - \"${1:8080}:${2:80}\"\n    volumes:\n      - ${./data}:/data\n    environment:\n      ${KEY}: ${value}",
    },
    {
      label: "healthcheck",
      template:
        'healthcheck:\n  test: ["CMD", "${1:true}"]\n  interval: 30s\n  timeout: 5s\n  retries: 3',
    },
    {
      label: "deployment",
      template:
        "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: ${name}\nspec:\n  replicas: ${1:1}\n  selector:\n    matchLabels:\n      app: ${name}\n  template:\n    metadata:\n      labels:\n        app: ${name}\n    spec:\n      containers:\n        - name: ${name}\n          image: ${image}",
    },
    {
      label: "service",
      template:
        "apiVersion: v1\nkind: Service\nmetadata:\n  name: ${name}\nspec:\n  selector:\n    app: ${name}\n  ports:\n    - port: ${1:80}\n      targetPort: ${2:8080}",
    },
    { label: "key-value", template: "${key}: ${value}" },
  ],
  nginx: [
    {
      label: "server",
      template:
        "server {\n  listen ${1:80};\n  server_name ${example.com};\n\n  location / {\n    ${}\n  }\n}",
    },
    { label: "location", template: "location ${/} {\n  ${}\n}" },
    {
      label: "proxy",
      template:
        "location / {\n  proxy_pass ${http://127.0.0.1:8080};\n  proxy_set_header Host $host;\n  proxy_set_header X-Real-IP $remote_addr;\n  proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n  proxy_set_header X-Forwarded-Proto $scheme;\n}",
    },
    { label: "upstream", template: "upstream ${name} {\n  server ${127.0.0.1:8080};\n}" },
    {
      label: "tls",
      template:
        "listen 443 ssl;\nssl_certificate ${/path/fullchain.pem};\nssl_certificate_key ${/path/privkey.pem};",
    },
    {
      label: "static",
      template:
        "location / {\n  root ${/var/www/html};\n  index index.html;\n  try_files $uri $uri/ =404;\n}",
    },
    {
      label: "websocket",
      template:
        'location ${/ws} {\n  proxy_pass ${http://127.0.0.1:8080};\n  proxy_http_version 1.1;\n  proxy_set_header Upgrade $http_upgrade;\n  proxy_set_header Connection "upgrade";\n}',
    },
  ],
};

function keywordLanguage(language: EditorLanguageId): EditorLanguageId {
  if (language === "vue") {
    return "html";
  }
  if (language === "scss" || language === "less") {
    return "css";
  }
  return language;
}

const keywordCache = new Map<EditorLanguageId, Completion[]>();
const snippetCache = new Map<EditorLanguageId, Completion[]>();

export function snippetSpecs(language: EditorLanguageId): readonly SnippetSpec[] {
  return SNIPPETS[language] ?? [];
}

export function keywordCompletions(language: EditorLanguageId): Completion[] {
  let options = keywordCache.get(language);
  if (!options) {
    options = (KEYWORDS[keywordLanguage(language)] ?? []).map((label) => ({
      label,
      type: "keyword",
    }));
    keywordCache.set(language, options);
  }
  return options;
}

export function snippetCompletions(language: EditorLanguageId): Completion[] {
  let options = snippetCache.get(language);
  if (!options) {
    options = snippetSpecs(language).map((snippet) =>
      snippetCompletion(snippet.template, {
        label: snippet.label,
        detail: "snippet",
        type: "keyword",
      }),
    );
    snippetCache.set(language, options);
  }
  return options;
}

export function completionSources(
  settings: EditorSettings,
  language: EditorLanguageId,
): CompletionSource[] {
  if (!settings.autocomplete) {
    return [];
  }
  const sources: CompletionSource[] = [];
  const keywords = keywordCompletions(language);
  if (keywords.length > 0) {
    sources.push(completeFromList(keywords));
  }
  const snippets = snippetCompletions(language);
  if (settings.snippets && snippets.length > 0) {
    sources.push(completeFromList(snippets));
  }
  sources.push(completeAnyWord);
  return sources;
}

export function completionExtensions(
  settings: EditorSettings,
  language: EditorLanguageId,
): Extension {
  if (!settings.autocomplete) {
    return [];
  }
  return [
    autocompletion({ override: completionSources(settings, language) }),
    keymap.of([
      {
        key: "Tab",
        run: (view) =>
          completionStatus(view.state) === "active" && acceptCompletion(view),
      },
    ]),
  ];
}

export function bracketExtensions(): Extension {
  return [closeBrackets(), keymap.of(closeBracketsKeymap)];
}
