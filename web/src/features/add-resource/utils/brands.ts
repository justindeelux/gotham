import { cardMark } from "@/features/templates/utils/templateMark";

export interface BrandMark {
  logo: string | null;
  letters: string;
  color: string;
}

const BRAND_ACCENT = "#5865f2";

export type BrandIconSize = "sm" | "md" | "lg";

const TEMPLATE_BRANDS: Record<string, { logo: string; color: string }> = {
  wordpress: { logo: "wordpress", color: "#21759b" },
  nextcloud: { logo: "nextcloud", color: "#0082c9" },
  n8n: { logo: "n8n", color: "#ea4b71" },
  "uptime-kuma": { logo: "uptimekuma", color: "#369b6b" },
};

const MARKS: Record<string, { logo: string | null; color: string }> = {
  git_public: { logo: "git", color: "#f05032" },
  git_private: { logo: "git", color: "#f05032" },
  github_app: { logo: "github", color: "#6e7681" },
  gitlab_app: { logo: "gitlab", color: "#fc6d26" },
  dockerfile: { logo: "docker", color: "#2496ed" },
  image: { logo: "docker", color: "#2496ed" },
  compose: { logo: "docker", color: "#2496ed" },
  postgres: { logo: "postgresql", color: "#4169e1" },
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
  const brand = TEMPLATE_BRANDS[icon];
  return {
    logo: brand?.logo ?? null,
    letters: cardMark(name),
    color: brand?.color ?? BRAND_ACCENT,
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
