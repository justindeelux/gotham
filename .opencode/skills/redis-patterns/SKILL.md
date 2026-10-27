---
name: redis-patterns
description: Redis data structure patterns, caching strategies, distributed locks, rate limiting, pub/sub, and connection management for production applications. Use when adding caching, a distributed lock, rate limiting, or pub/sub with Redis, or when key design needs review.
metadata:
  origin: ECC
---

# Redis Patterns

Quick reference for Redis best practices across common backend use cases.

## How It Works

Redis is an in-memory data structure store that supports strings, hashes, lists, sets, sorted sets, streams, and more. Individual Redis commands are atomic on a single instance; multi-step workflows require Lua scripts, MULTI/EXEC transactions, or explicit synchronization to stay atomic. Data is optionally persisted via RDB snapshots or AOF logs. Clients communicate over TCP using the RESP protocol; connection pools are essential to avoid per-request handshake overhead.

## When to Activate

- Adding caching to an application
- Implementing rate limiting or throttling
- Building distributed locks or coordination
- Setting up session or token storage
- Using Pub/Sub or Redis Streams for messaging
- Configuring Redis in production (pooling, eviction, clustering)

## Data Structure Cheat Sheet

| Use Case | Structure | Example Key |
|----------|-----------|-------------|
| Simple cache | String | `product:123` |
| User session | Hash | `session:abc` |
| Leaderboard | Sorted Set | `scores:weekly` |
| Unique visitors | Set | `visitors:2024-01-01` |
| Activity feed | List | `feed:user:456` |
| Event stream | Stream | `events:orders` |
| Counters / rate limits | String (INCR) | `ratelimit:user:123` |
| Bloom filter / HLL | HyperLogLog | `hll:pageviews` |

## Core Patterns

### Cache-Aside (Lazy Loading)

Redis command sequence (JSON encoding/decoding stays in Go):

```text
GET product:123
# On a miss, load the product from PostgreSQL, then:
SET product:123 '{"id":123,"name":"Example"}' EX 3600
```

Treat a cache miss separately from a Redis connection error. Use the existing
go-redis client and propagate or handle failures according to the cache's policy.

### Write-Through Cache

After the PostgreSQL write succeeds, update the serialized cache value:

```text
SET product:123 '{"id":123,"name":"Updated"}' EX 3600
```

Handle cache-write failure explicitly; the database write has already committed.

### Cache Invalidation

Tag-based invalidation groups related keys under a set:

```text
MULTI
SET product:123 '{"id":123,"category_id":7}' EX 3600
SADD tag:category:7 product:123
EXPIRE tag:category:7 3600
EXEC

# To invalidate: read the set, delete the returned keys, then remove the tag.
SMEMBERS tag:category:7
DEL product:123
DEL tag:category:7
```

The Go caller builds the deletion from the returned set members. Bound the set size;
concurrent writers require a stronger invalidation scheme when strict consistency matters.

### Session Storage

Generate an unpredictable session ID in Go, then store and expire its hash atomically:

```text
MULTI
HSET session:generated-id user_id 123 created_at 1700000000
EXPIRE session:generated-id 86400
EXEC

HGETALL session:generated-id
DEL session:generated-id
```

This is a Redis storage pattern, not a requirement to move existing sessions out of
PostgreSQL. Preserve the application's authentication/session design.

## Rate Limiting

### Fixed Window (Simple)

Derive a key from the user ID and current time bucket in Go, then:

```text
MULTI
INCR ratelimit:123:time-bucket
EXPIRE ratelimit:123:time-bucket 60
EXEC
```

Compare the returned count with the limit. Use a transaction (`TxPipeline` in go-redis),
not an ordinary pipeline, and distinguish command errors from an allowed request.

### Sliding Window (Lua — Atomic)

```lua
-- sliding_window.lua
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])

redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local count = redis.call('ZCARD', key)

if count < limit then
    -- Use unique member (now + sequence) to avoid collisions within the same millisecond
    local seq_key = key .. ':seq'
    local seq = redis.call('INCR', seq_key)
    redis.call('EXPIRE', seq_key, math.ceil(window / 1000))
    redis.call('ZADD', key, now, now .. '-' .. seq)
    redis.call('EXPIRE', key, math.ceil(window / 1000))
    return 1
end
return 0
```

```bash
# now_ms is the current Unix time in milliseconds supplied by the caller.
redis-cli --eval sliding_window.lua ratelimit:sliding:123 , "$now_ms" 60000 100
```

The Go caller can execute the same Lua script through its existing Redis client.
A result of 1 allows the request; 0 denies it. Handle execution errors explicitly.

## Distributed Locks

### Distributed Lock (Single Node — SET NX PX)

Generate a unique ownership token for every acquisition in Go:

```text
SET lock:order:123 unique-owner-token NX PX 5000
```

A nil result means another owner holds the lock. Release atomically only if the token
still matches; an expired owner must not delete its successor's lock:

```lua
if redis.call('GET', KEYS[1]) == ARGV[1] then
    return redis.call('DEL', KEYS[1])
end
return 0
```

Use `defer` for release in the Go caller. The TTL must cover the work or be safely
renewed; expiry does not stop the original worker.

> For coordination across multiple Redis nodes, choose a mechanism that meets the required failure guarantees; do not assume a single-node lease remains safe through failover.

## Pub/Sub & Streams

### Pub/Sub (Fire-and-Forget)

```text
# Subscriber connection:
SUBSCRIBE events:orders

# Separate publisher connection:
PUBLISH events:orders '{"id":123,"status":"created"}'
```

In Go, own the subscription's context and goroutine, handle disconnection, and close it
on shutdown. Pub/Sub does not retain events for disconnected subscribers.

### Redis Streams (Durable Queue)

```text
# Producer (bounded stream):
XADD events:orders MAXLEN ~ 10000 * order_id 123 status created

# Create once; only BUSYGROUP means the group already exists.
XGROUP CREATE events:orders processor 0 MKSTREAM

# Consumer:
XREADGROUP GROUP processor worker-1 COUNT 10 BLOCK 2000 STREAMS events:orders >
# Process each entry successfully before acknowledging its returned ID:
XACK events:orders processor returned-entry-id
```

Handle command failures, cancellation, and pending-entry recovery in the Go consumer.
Replayed work must be idempotent for at-least-once processing.

> Prefer **Streams** over Pub/Sub when you need delivery guarantees, consumer groups, or replay.

## Key Design

### Naming Conventions

```
# Pattern: resource:id:field
user:123:profile
order:456:status
cache:product:789

# Pattern: namespace:resource:id
myapp:session:abc123
myapp:ratelimit:user:123

# Pattern: resource:date (time-bound keys)
stats:pageviews:2024-01-01
```

### TTL Strategy

| Data Type | Suggested TTL |
|-----------|--------------|
| User session | 24h (`86400`) |
| API response cache | 5–15 min |
| Rate limit window | Match window size |
| Short-lived tokens | 5–10 min |
| Leaderboard | 1h–24h |
| Static/reference data | 1h–1 week |

Always set a TTL. Keys without TTL accumulate indefinitely and cause memory pressure.

## Connection Management

### Connection Pooling

```go
import (
    "time"

    "github.com/redis/go-redis/v9"
)

client := redis.NewClient(&redis.Options{
    Addr:         "localhost:6379",
    DB:           0,
    PoolSize:     20,
    DialTimeout:  2 * time.Second,
    ReadTimeout:  2 * time.Second,
    WriteTimeout: 2 * time.Second,
})
// Share the client; close it when its owning service shuts down.
```

### Cluster Mode

For a Redis Cluster deployment, use the Go client's cluster support and supply its
startup addresses. Command-line diagnosis follows redirects with:

```bash
redis-cli -c -h redis-1 -p 6379 PING
```

Multi-key transactions and Lua scripts require compatible key hash slots.

### Sentinel (High Availability)

For a Sentinel-managed deployment, configure the Go client's failover support with the
master name and Sentinel addresses. Discover the current primary with:

```bash
redis-cli -h sentinel-1 -p 26379 SENTINEL get-master-addr-by-name mymaster
```

## Eviction Policies

| Policy | Behavior | Best For |
|--------|----------|----------|
| `noeviction` | Error on write when full | Queues / critical data |
| `allkeys-lru` | Evict least recently used | General cache |
| `volatile-lru` | LRU only among keys with TTL | Mixed data store |
| `allkeys-lfu` | Evict least frequently used | Skewed access patterns |
| `volatile-ttl` | Evict soonest-to-expire | Prioritize long-lived data |

Set via `redis.conf`: `maxmemory-policy allkeys-lru`

## Anti-Patterns

| Anti-Pattern | Problem | Fix |
|---|---|---|
| Keys with no TTL | Memory grows unbounded | Always set TTL |
| `KEYS *` in production | Blocks the server (O(N)) | Use `SCAN` cursor |
| Storing large blobs (>100KB) | Slow serialization, memory pressure | Store reference + fetch from object store |
| Single Redis for everything | No isolation between cache & queue | Use separate DBs or instances |
| Ignoring connection pool limits | Connection exhaustion under load | Size pool to workload |
| Not handling cache miss stampede | Thundering herd on cold start | Use locks or probabilistic early expiry |
| `FLUSHALL` without thought | Wipes entire instance | Scope deletes by key pattern |

### Cache Miss Stampede Prevention

For a single Go process:

1. Read the cache.
2. Coalesce concurrent misses for the same key with the existing `singleflight.Group`
   from `golang.org/x/sync/singleflight`.
3. Recheck the cache inside the shared call.
4. Fetch from the authoritative source only on a miss.
5. Cache the result with a TTL and return it; handle fetch and cache errors explicitly.

Keep unrelated keys independent and honor caller cancellation.

> Note: for multi-process deployments, use an ownership-token lease from the Distributed Locks section above instead of relying only on in-process coalescing.

## Examples

**Add caching to a Go API endpoint:**
Use cache-aside with `SET ... EX` and a 5-minute TTL on the response. Key on the request parameters.

**Rate-limit an API by user:**
Use fixed-window with a transaction for low-traffic endpoints; use sliding-window Lua for accurate per-user throttling.

**Coordinate a background job across workers:**
Use an ownership-token lease with a TTL that exceeds the expected job duration. Release with an atomic token check in a deferred Go cleanup.

**Fan-out notifications to multiple subscribers:**
Use Pub/Sub for fire-and-forget. Switch to Streams if you need guaranteed delivery or replay for late consumers.

## Quick Reference

| Pattern | When to Use |
|---------|-------------|
| Cache-aside | Read-heavy, tolerate slight staleness |
| Write-through | Strong consistency required |
| Distributed lock | Prevent concurrent access to a resource |
| Sliding window rate limit | Accurate per-user throttling |
| Redis Streams | Durable event queue with consumer groups |
| Pub/Sub | Broadcast with no delivery guarantees needed |
| Sorted Set leaderboard | Ranked scoring, pagination |
| HyperLogLog | Approximate unique count at low memory |

## Related

- Skill: `postgres-patterns` — relational data patterns
- Skill: `golang-patterns` — Go concurrency and resource ownership
