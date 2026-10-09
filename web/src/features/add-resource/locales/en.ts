/**
 * English catalog for the Add-resource picker (GS-1). Display copy only:
 * template slugs, engine values, image tags and ports stay raw values
 * interpolated as parameters. Template card descriptions resolve through
 * the templates overlay with fallback to provider metadata.
 */
const en = {
  search: {
    label: "Filter services",
    placeholder: "Filter services…",
    empty: "No service matches this filter.",
    emptyCatalog: "No templates in the catalog.",
  },
  groups: {
    application: "Application",
    service: "Service",
    database: "Database",
  },
  sources: {
    git_public: "Deploy a public repository by URL, no connection needed.",
    git_private: "Deploy a private repository with a deploy key or token.",
    github_app: "Deploy from a connected GitHub account.",
    gitlab_app: "Deploy from a connected GitLab account.",
    dockerfile: "Build from pasted Dockerfile text, no repository needed.",
    image: "Run a prebuilt container image from any registry.",
    compose: "Run services from a Docker Compose file.",
  },
  engines: {
    postgres: "Reliable relational database for application data.",
    mysql: "Popular relational database for web workloads.",
    mariadb: "Community MySQL fork with a compatible wire protocol.",
    mongodb: "Document database for flexible JSON-like data.",
    redis: "In-memory cache and queue with optional persistence.",
  },
};
export default en;
