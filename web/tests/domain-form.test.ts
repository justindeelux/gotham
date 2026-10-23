import { describe, expect, it } from "vitest";

import {
  domainPortSchema,
  domainProtocolSchema,
  domainServiceSchema,
  hostDomainSchema,
} from "@/features/applications/schemas/applications";
import { extractComposeServicePorts } from "@/features/applications/utils/compose";

describe("domain routing schemas", () => {
  it("accepts 1-65535 target ports only", () => {
    expect(domainPortSchema.safeParse(1).success).toBe(true);
    expect(domainPortSchema.safeParse(65535).success).toBe(true);
    expect(domainPortSchema.safeParse(0).success).toBe(false);
    expect(domainPortSchema.safeParse(65536).success).toBe(false);
    expect(domainPortSchema.safeParse(null).success).toBe(false);
    expect(domainPortSchema.safeParse(1.5).success).toBe(false);
  });

  it("accepts http and https only", () => {
    expect(domainProtocolSchema.safeParse("http").success).toBe(true);
    expect(domainProtocolSchema.safeParse("https").success).toBe(true);
    expect(domainProtocolSchema.safeParse("tcp").success).toBe(false);
    expect(domainProtocolSchema.safeParse("").success).toBe(false);
  });

  it("allows an empty service (release default) or a valid compose name", () => {
    expect(domainServiceSchema.safeParse("").success).toBe(true);
    expect(domainServiceSchema.safeParse("web").success).toBe(true);
    expect(domainServiceSchema.safeParse("api_v2.0-x").success).toBe(true);
    expect(domainServiceSchema.safeParse("-bad").success).toBe(false);
    expect(domainServiceSchema.safeParse("has space").success).toBe(false);
  });

  it("rejects an empty hostname but accepts a plain host", () => {
    expect(hostDomainSchema.safeParse("app.example.com").success).toBe(true);
    expect(hostDomainSchema.safeParse("").success).toBe(true);
    expect(hostDomainSchema.safeParse("not a domain").success).toBe(false);
  });
});

describe("compose service port pre-fill", () => {
  it("maps each service to its declared container port", () => {
    const ports = extractComposeServicePorts(`services:
  web:
    image: example.com/app:1.0
    ports:
      - "8080:3000"
  api:
    image: example.com/api:1.0
    ports:
      - target: 8000
        published: 18000
  worker:
    image: example.com/worker:1.0
`);
    expect(ports.web).toBe(3000);
    expect(ports.api).toBe(8000);
    expect(ports.worker).toBe(0);
  });

  it("only reads port/expose lists, not unrelated service lists", () => {
    const ports = extractComposeServicePorts(`services:
  web:
    image: example.com/app:1.0
    volumes:
      - 3000:/data
    command:
      - 8080
    ports:
      - "9000-9010:9000"
  api:
    image: example.com/api:1.0
    expose:
      - "7000"
`);
    expect(ports.web).toBe(9000);
    expect(ports.api).toBe(7000);
  });
});
