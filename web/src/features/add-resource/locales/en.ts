const en = {
  tabs: {
    label: "Resource type",
    application: "Application",
    service: "Service",
    database: "Database",
  },
  panels: {
    application: "Pick a source to deploy as an application.",
    service: "Pick a service template, or import a Docker Compose file.",
    database: "Pick a database engine to provision.",
  },
  search: {
    label: "Filter services",
    placeholder: "Filter services…",
    empty: "No service matches this filter.",
    emptyCatalog: "No templates in the catalog.",
  },
  compose: {
    hint: "Have a Docker Compose file?",
    link: "Import a compose file",
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
