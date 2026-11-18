import { cardMark } from "@/features/templates/utils/templateMark";
import { useTemplatesStore } from "@/features/templates/stores/templates";
import type { Service } from "@/features/services/api/services";

export interface BrandMark {
  logo: string | null;
  letters: string;
  color: string;
}

const BRAND_ACCENT = "#5865f2";

export type BrandIconSize = "sm" | "md" | "lg";

// Engine logos resolve to the catalog icons named after the engine value.
const MARKS: Record<string, { logo: string | null; color: string }> = {
  git_public: { logo: "git", color: "#f05032" },
  git_private: { logo: "git", color: "#f05032" },
  github_app: { logo: "github", color: "#6e7681" },
  gitlab_app: { logo: "gitlab", color: "#fc6d26" },
  dockerfile: { logo: "docker", color: "#2496ed" },
  image: { logo: "docker", color: "#2496ed" },
  compose: { logo: "docker", color: "#2496ed" },
  postgres: { logo: "postgres", color: "#4169e1" },
  mysql: { logo: "mysql", color: "#4479a1" },
  mariadb: { logo: "mariadb", color: "#c0765a" },
  mongodb: { logo: "mongodb", color: "#47a248" },
  redis: { logo: "redis", color: "#ff4438" },
};

export function initialsOf(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) {
    return "?";
  }
  if (words.length === 1) {
    return words[0].slice(0, 2).toUpperCase();
  }
  return (words[0][0] + words[1][0]).toUpperCase();
}

export function templateBrand(icon: string, name: string): BrandMark {
  return {
    logo: icon === "" ? null : icon,
    letters: cardMark(name),
    color: BRAND_ACCENT,
  };
}

export function brandFor(key: string, name: string): BrandMark {
  const brand = MARKS[key];
  return {
    logo: brand?.logo ?? null,
    letters: initialsOf(name),
    color: brand?.color ?? BRAND_ACCENT,
  };
}

export function applicationBrand(
  sourceType: string,
  provider: string,
  name: string,
): BrandMark {
  if (sourceType !== "") {
    return brandFor(sourceType, name);
  }
  if (provider === "github") {
    return brandFor("github_app", name);
  }
  if (provider === "gitlab") {
    return brandFor("gitlab_app", name);
  }
  return brandFor("git_public", name);
}

export function databaseBrand(engine: string, name: string): BrandMark {
  return brandFor(engine, name);
}

export function serviceBrand(service: Pick<Service, "name" | "template">): BrandMark {
  const template = (service.template ?? "").trim();
  if (template === "") {
    return brandFor("compose", service.name);
  }
  const listed = useTemplatesStore().templates.find((item) => item.slug === template);
  return templateBrand(listed?.icon ?? template, service.name);
}
