/**
 * English catalog for the Add-resource picker (GS-1). Display copy only:
 * template slugs, engine values, image tags and ports stay raw values
 * interpolated as parameters. Template card descriptions resolve through
 * the templates overlay with fallback to provider metadata.
 */
const en = {
  page: {
    eyebrow: "Operations · new resource",
    title: "Add resource",
    description:
      "Pick what to create. An application builds and deploys from source, a service deploys a one-click template, a database provisions a managed engine. Each card opens the existing create flow.",
    scopeIn: "Creating in {project} · {environment} — the wizards keep this scope.",
    scopeGlobal: "Pick the project and environment inside the wizard.",
  },
  search: {
    label: "Filter services",
    placeholder: "Filter services…",
    empty: "No service matches this filter.",
    emptyCatalog: "No templates in the catalog.",
  },
  groups: {
    application: "Application",
    applicationMeta: "wizard · git repository",
    service: "Service",
    serviceMeta: "one card per template",
    serviceHint:
      "One card per template from the template catalog. Selecting a card opens the template deploy wizard with the template preselected.",
    database: "Database",
    databaseMeta: "one card per engine",
    databaseHint:
      "One card per supported engine. Selecting a card opens the database wizard with the engine preselected.",
  },
  applicationCard: {
    name: "Application",
    description: "Build and deploy from a Git repository.",
    tag: "source wizard",
  },
  serviceCard: {
    tag: "template",
  },
  engines: {
    postgres: "Reliable relational database for application data.",
    mysql: "Popular relational database for web workloads.",
    mariadb: "Community MySQL fork with a compatible wire protocol.",
    mongodb: "Document database for flexible JSON-like data.",
    redis: "In-memory cache and queue with optional persistence.",
  },
  keyboardHint:
    "Keyboard: Tab moves between cards, arrow keys move inside a group, Enter opens the create flow.",
};

export default en;
