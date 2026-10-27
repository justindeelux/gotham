---
name: docker-patterns
description: Docker and Docker Compose patterns for local development, hardened CLI installer harnesses, container security, networking, volumes, and multi-service orchestration. Use when creating or reviewing Dockerfiles and Compose services, testing installers across Linux distributions, or planning accurate native macOS and Windows validation.
---

# Docker Patterns

Docker and Docker Compose best practices for containerized development.

## Docker Compose for Local Development

### Standard Web App Stack

```yaml
# docker-compose.yml
services:
  app:
    build:
      context: .
      target: dev                     # Use dev stage of multi-stage Dockerfile
    ports:
      - "8000:8000"
    volumes:
      - .:/app                        # Bind mount for hot reload
      - go-mod-cache:/go/pkg/mod       # Preserve downloaded Go modules
    environment:
      - DATABASE_URL=postgres://postgres:postgres@db:5432/app_dev
      - REDIS_URL=redis://redis:6379/0
    depends_on:
      db:
        condition: service_healthy
      redis:
        condition: service_started
    command: go run ./cmd/gotham serve

  db:
    image: postgres:16-alpine
    ports:
      - "127.0.0.1:5432:5432"
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: app_dev
    volumes:
      - pgdata:/var/lib/postgresql/data
      - ./scripts/init-db.sql:/docker-entrypoint-initdb.d/init.sql
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 5s
      timeout: 3s
      retries: 5

  redis:
    image: redis:7-alpine
    ports:
      - "127.0.0.1:6379:6379"
    volumes:
      - redisdata:/data


volumes:
  pgdata:
  redisdata:
  go-mod-cache:
```

### Development vs Production Dockerfile

```dockerfile
# Match the Go image version to go.mod.
FROM golang:1.27-alpine AS deps
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# Stage: dev
FROM deps AS dev
COPY . .
EXPOSE 8000
CMD ["go", "run", "./cmd/gotham", "serve"]

# Stage: build
FROM deps AS build
COPY . .
RUN CGO_ENABLED=0 go build -o /out/app ./cmd/gotham

# Stage: production (no Go or Node runtime required)
FROM alpine:3.22 AS production
RUN apk add --no-cache ca-certificates && \
    addgroup -g 1001 -S appgroup && adduser -S appuser -u 1001 -G appgroup
USER appuser
COPY --from=build --chown=appuser:appgroup /out/app /usr/local/bin/app
EXPOSE 8000
CMD ["app", "serve"]
```

If the Go binary embeds the Vue SPA, build
the frontend with Node/npm before the Go build and include its generated assets.
Node is a frontend build dependency, not the production HTTP server.

### Override Files

```yaml
# docker-compose.override.yml (auto-loaded, dev-only settings)
services:
  app:
    environment:
      - LOG_LEVEL=debug

# docker-compose.prod.yml (explicit for production)
services:
  app:
    build:
      target: production
    restart: always
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 512M
```

```bash
# Development (auto-loads override)
docker compose up

# Production
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

## Networking

### Service Discovery

Services in the same Compose network resolve by service name:
```
# From "app" container:
postgres://postgres:postgres@db:5432/app_dev    # "db" resolves to the db container
redis://redis:6379/0                             # "redis" resolves to the redis container
```

### Custom Networks

```yaml
services:
  frontend:
    networks:
      - frontend-net

  api:
    networks:
      - frontend-net
      - backend-net

  db:
    networks:
      - backend-net              # Only reachable from api, not frontend

networks:
  frontend-net:
  backend-net:
```

### Exposing Only What's Needed

```yaml
services:
  db:
    ports:
      - "127.0.0.1:5432:5432"   # Only accessible from host, not network
    # Omit ports entirely in production -- accessible only within Docker network
```

## Volume Strategies

```yaml
volumes:
  # Named volume: persists across container restarts, managed by Docker
  pgdata:

  # Bind mount: maps host directory into container (for development)
  # - ./src:/app/src

  # Anonymous volume: preserves container-generated content from bind mount override
  # - /go/pkg/mod
```

### Common Patterns

```yaml
services:
  app:
    volumes:
      - .:/app                   # Source code (bind mount for hot reload)
      - /go/pkg/mod              # Preserve downloaded Go modules
      - /root/.cache/go-build    # Preserve Go build cache

  db:
    volumes:
      - pgdata:/var/lib/postgresql/data          # Persistent data
      - ./scripts/init.sql:/docker-entrypoint-initdb.d/init.sql  # Init scripts
```

## Container Security

### Dockerfile Hardening

```dockerfile
# 1. Use specific tags (never :latest)
FROM alpine:3.22

# 2. Run as non-root
RUN addgroup -g 1001 -S app && adduser -S app -u 1001
USER app

# 3. Drop capabilities (in compose)
# 4. Read-only root filesystem where possible
# 5. No secrets in image layers
```

### Compose Security

```yaml
services:
  app:
    security_opt:
      - no-new-privileges:true
    read_only: true
    tmpfs:
      - /tmp
      - /app/.cache
    cap_drop:
      - ALL
    cap_add:
      - NET_BIND_SERVICE          # Only if binding to ports < 1024
```

### Secret Management

```yaml
# GOOD: Use environment variables (injected at runtime)
services:
  app:
    env_file:
      - .env                     # Never commit .env to git
    environment:
      - API_KEY                  # Inherits from host environment

# GOOD: Compose secrets (mounted as files; the application must support file-based secrets)
secrets:
  db_password:
    file: ./secrets/db_password.txt

services:
  db:
    secrets:
      - db_password

# BAD: Hardcoded in image
# ENV API_KEY=sk-proj-xxxxx      # NEVER DO THIS
```

## Hardened CLI Installer Harnesses

Use containers to test installer behavior against disposable project copies without allowing the test to mutate the source checkout.

### Respect the Platform Boundary

- Run real containers for Linux distributions such as Debian and Ubuntu.
- macOS cannot run as a Docker container because Docker shares a Linux kernel. Run the same shell-free test entry point natively on macOS.
- Windows containers require a Windows Docker engine. Run platform-independent logic on a native Windows CI runner and reserve Windows containers for a Windows host.
- Keep a native Ubuntu/macOS/Windows CI matrix for host-specific paths, command shims, quoting, and filesystem behavior.

Do not claim that a Linux container validates macOS or Windows behavior.

### Enforce the Isolation Contract

- Pin base images by immutable digest and pin installed CLI versions.
- Run as a non-root numeric UID/GID when distro account names differ.
- Mount the repository and source project read-only.
- Copy the source project into a writable `tmpfs` workspace before any mutation.
- Mount `/workspace` with `noexec`, UID/GID 1000, and `mode=0700` so only the
  container user can inspect project data.
- Set `read_only: true`, `no-new-privileges:true`, `cap_drop: [ALL]`, and a finite `pids_limit`.
- Keep the default real-CLI services on `network_mode: none`. Add network access
  only through a visibly named opt-in service for an authenticated provider
  session; never make it an accidental environment-driven default.
- Create only the writable temporary paths the tool needs.
- Do not pass host credentials into the container by default.
- Use the installer's supported dry-run mode when available. Do not assume a dry run
  exists; run real installs only in the disposable test target.
- Use argument arrays, such as `exec.CommandContext` in Go, for test runners.
  Never interpolate project paths into a shell command.

## .dockerignore

```
node_modules
.git
.env
.env.*
dist
bin
coverage
*.log
.cache
docker-compose*.yml
Dockerfile*
README.md
```

## Debugging

### Common Commands

```bash
# View logs
docker compose logs -f app           # Follow app logs
docker compose logs --tail=50 db     # Last 50 lines from db

# Execute commands in running container
docker compose exec app sh           # Shell into app
docker compose exec db psql -U postgres  # Connect to postgres

# Inspect
docker compose ps                     # Running services
docker compose top                    # Processes in each container
docker stats                          # Resource usage

# Rebuild
docker compose up --build             # Rebuild images
docker compose build --no-cache app   # Force full rebuild

# Clean up
docker compose down                   # Stop and remove containers
docker compose down -v                # Also remove volumes (DESTRUCTIVE)
docker system prune                   # Remove unused images/containers
```

### Debugging Network Issues

```bash
# Check DNS resolution inside container
docker compose exec app nslookup db

# Check connectivity
docker compose exec app wget -qO- http://api:8000/health

# Inspect network
docker network ls
docker network inspect <project>_default
```

## Anti-Patterns

```
# BAD: Introducing a mandatory orchestrator for a supported Compose deployment
# Docker Compose is valid for self-hosted workloads; choose topology for the actual need

# BAD: Storing data in containers without volumes
# Containers are ephemeral -- all data lost on restart without volumes

# BAD: Running as root
# Use a non-root user where possible; keep explicitly privileged host/agent work isolated

# BAD: Using :latest tag
# Pin to specific versions for reproducible builds

# BAD: One giant container with all services
# Separate concerns: one process per container

# BAD: Putting secrets in docker-compose.yml
# Use .env files (gitignored) or Docker secrets
```
