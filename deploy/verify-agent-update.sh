#!/bin/sh
#
# verify-agent-update.sh proves the BE-9.2 agent remote-update flow end to end on
# a real Linux host with systemd and Docker. It is the merge gate for PR #72.
#
#   sudo GOTHAM_GO=/path/to/go sh deploy/verify-agent-update.sh
#
# It builds the control plane and two agent versions from this repository, signs
# a release with the embedded-key trust anchor, serves it from a loopback HTTP
# server, runs a scratch control plane plus two scratch agents as systemd units,
# and drives the operator "update all agents" API.
#
# Everything is scratch: scratch units, a scratch user, a scratch database, a
# scratch port range and a scratch directory, all removed on exit. It never
# touches a real gotham/gotham-agent unit, database, certificate or binary.
#
# What it proves (the reviewer's three merge confirmations, plus negatives):
#   C1 update-all with the control plane already current starts a rollout
#   C2 both agents restart through the root-owned wrapper and record ok
#   C3 both heartbeats converge on the new version, planted install byte-identical
#   C4 the control plane resolves the *agent* release family
#   NEG1 a tampered asset (digest mismatch) leaves the agents on the old version
#   NEG2 a validly signed but broken release rolls back and records rolled_back
#
# Environment:
#   GOTHAM_GO             Go binary (default: go)
#   VERIFY_DATABASE_URL   base Postgres DSN; a scratch database is created
#                         (default: postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable)
#   VERIFY_PG_CONTAINER   Postgres container for createdb/dropdb (default: gotham-dev-postgres)
#   VERIFY_PG_USER        Postgres superuser in that container (default: gotham)
#   VERIFY_HTTP_PORT      override the scratch HTTP port
#   VERIFY_GRPC_PORT      override the scratch gRPC port
#   VERIFY_RELEASE_PORT   override the loopback release-server port
#   VERIFY_KEEP=1         skip the cleanup trap and keep the scratch dir + logs
#                         for debugging (prints the paths)
#
# Exit: 0 when every check passes, 1 otherwise.
set -u

# ---------------------------------------------------------------------------
# Preconditions
# ---------------------------------------------------------------------------
if [ "$(id -u)" -ne 0 ]; then
    echo "verify-agent-update.sh must run as root (try sudo)" >&2
    exit 1
fi

GOTHAM_GO="${GOTHAM_GO:-go}"
for tool in systemctl docker python3 curl flock md5sum stat useradd usermod sudo; do
    if ! command -v "${tool}" >/dev/null 2>&1; then
        echo "verify-agent-update.sh: ${tool} is required" >&2
        exit 1
    fi
done

REPO_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ ! -f "${REPO_DIR}/go.mod" ] || [ ! -f "${REPO_DIR}/deploy/gotham-update.sh" ]; then
    echo "verify-agent-update.sh: run it from the repository (${REPO_DIR})" >&2
    exit 1
fi

RUN_ID=$$
SCRATCH=$(mktemp -d "/tmp/gotham-agent-verify.${RUN_ID}.XXXXXX") || {
    echo "verify-agent-update.sh: could not create a scratch directory" >&2
    exit 1
}
if [ -z "${SCRATCH}" ] || [ "${SCRATCH}" = "/" ]; then
    echo "verify-agent-update.sh: refusing to run with an unsafe scratch directory" >&2
    exit 1
fi
chmod 0755 "${SCRATCH}"

# Ports: a high scratch range, far from the real CP/agent ports (8000/9442/9443)
# and the compose ports (5433/6380/8099/9444).
BASE_PORT=$(( 30000 + (RUN_ID % 20000) ))
HTTP_PORT="${VERIFY_HTTP_PORT:-$((BASE_PORT + 0))}"
GRPC_PORT="${VERIFY_GRPC_PORT:-$((BASE_PORT + 1))}"
HEALTH_A_PORT=$((BASE_PORT + 2))
HEALTH_B_PORT=$((BASE_PORT + 3))
RELEASE_PORT="${VERIFY_RELEASE_PORT:-$((BASE_PORT + 4))}"
AGENT_LISTEN_A=$((BASE_PORT + 5))
AGENT_LISTEN_B=$((BASE_PORT + 6))

case "$(uname -m)" in
    x86_64 | amd64) ARCH=amd64 ;;
    aarch64 | arm64) ARCH=arm64 ;;
    *)
        echo "verify-agent-update.sh: unsupported architecture $(uname -m)" >&2
        exit 1
        ;;
esac

AGENT_USER="gvagent${RUN_ID}"
UNIT_A="gotham-agent-verify-${RUN_ID}-a"
UNIT_B="gotham-agent-verify-${RUN_ID}-b"
UNIT_DIR="/etc/systemd/system"
SUDOERS_FILE="/etc/sudoers.d/gotham-agent-verify-${RUN_ID}"
PG_CONTAINER="${VERIFY_PG_CONTAINER:-gotham-dev-postgres}"
PG_USER="${VERIFY_PG_USER:-gotham}"

BASE_DSN="${VERIFY_DATABASE_URL:-postgres://gotham:gotham@localhost:5432/gotham?sslmode=disable}"
BASE_DB="${BASE_DSN##*/}"
BASE_DB="${BASE_DB%%\?*}"
DSN_PREFIX="${BASE_DSN%/"${BASE_DB}"*}"
DSN_SUFFIX=""
case "${BASE_DSN}" in
    *"?"*) DSN_SUFFIX="?${BASE_DSN#*\?}" ;;
esac
SCRATCH_DB="${BASE_DB}_verify_${RUN_ID}"
SCRATCH_DSN="${DSN_PREFIX}/${SCRATCH_DB}${DSN_SUFFIX}"

CP_PID=""
RELEASE_PID=""
TOKEN=""
NODE_A="verify-node-a-${RUN_ID}"
NODE_B="verify-node-b-${RUN_ID}"
STATE_A="${SCRATCH}/agents/a"
STATE_B="${SCRATCH}/agents/b"
VERIFY_KEEP="${VERIFY_KEEP:-0}"
FAILURES=0

# ---------------------------------------------------------------------------
# Reporting and cleanup
# ---------------------------------------------------------------------------
pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; FAILURES=$((FAILURES + 1)); }
log() { echo "==> $*"; }

# timestamp_to_epoch converts a Go RFC3339Nano timestamp to epoch seconds, or 0
# when it cannot be parsed. Go trims trailing zeros, so the fraction may have any
# length from 1 to 9 digits; Python 3.10's fromisoformat accepts only 3 or 6, so
# pad/truncate it to exactly 6 before parsing (the box's Python rejected a
# 9-digit value and the helper always returned 0).
timestamp_to_epoch() { # $1 raw RFC3339
    python3 -c '
import sys, re, datetime
raw = (sys.argv[1] if len(sys.argv) > 1 else "").strip()
if not raw:
    print(0)
    raise SystemExit(0)
raw = re.sub(r"\.(\d+)", lambda m: "." + (m.group(1) + "000000")[:6], raw)
try:
    print(int(datetime.datetime.fromisoformat(raw.replace("Z", "+00:00")).timestamp()))
except Exception:
    print(0)
' "$1" 2>/dev/null
}

# self_check_timestamp fails the script early when the interpreter cannot parse
# the 9-digit fractional seconds Go emits.
self_check_timestamp() {
    sample="2026-09-30T12:00:05.123456789Z"
    got=$(timestamp_to_epoch "${sample}")
    want=$(python3 -c 'import datetime;print(int(datetime.datetime(2026,9,30,12,0,5,tzinfo=datetime.timezone.utc).timestamp()))' 2>/dev/null)
    if [ -n "${got}" ] && [ "${got}" != "0" ] && [ "${got}" = "${want}" ]; then
        return 0
    fi
    echo "timestamp self-check failed: ${sample} -> ${got:-<empty>} (want ${want:-?})" >&2
    return 1
}

if ! self_check_timestamp; then
    echo "verify-agent-update.sh: python3 cannot parse Go RFC3339Nano timestamps" >&2
    exit 1
fi

# print_debug_paths prints where the scratch artifacts live and the tail of the
# control-plane log, so a failure on a remote box is diagnosable without the
# cleanup trap having removed everything.
print_debug_paths() {
    echo "scratch dir: ${SCRATCH}" >&2
    echo "  control-plane log: ${SCRATCH}/cp.log" >&2
    echo "  release-server log: ${SCRATCH}/release.log" >&2
    if [ -f "${SCRATCH}/cp.log" ]; then
        echo "--- control-plane log (tail) ---" >&2
        tail -n 30 "${SCRATCH}/cp.log" >&2 || true
    fi
}

cleanup() {
    if [ "${VERIFY_KEEP}" = "1" ]; then
        echo "VERIFY_KEEP=1: leaving scratch artifacts in place" >&2
        print_debug_paths
        return
    fi
    # Stop the agents first so they stop restarting, then clear any failed/
    # rate-limited state and kill lingering scratch-user processes. Every step
    # is best-effort so a failing run still leaves the host clean.
    systemctl stop "${UNIT_A}" "${UNIT_B}" >/dev/null 2>&1 || true
    systemctl reset-failed "${UNIT_A}" "${UNIT_B}" >/dev/null 2>&1 || true
    if command -v pkill >/dev/null 2>&1; then
        pkill -u "${AGENT_USER}" >/dev/null 2>&1 || true
    fi
    rm -f "${UNIT_DIR}/${UNIT_A}.service" "${UNIT_DIR}/${UNIT_B}.service" "${SUDOERS_FILE}"
    systemctl daemon-reload >/dev/null 2>&1 || true
    if [ -n "${CP_PID}" ]; then kill "${CP_PID}" >/dev/null 2>&1 || true; fi
    if [ -n "${RELEASE_PID}" ]; then kill "${RELEASE_PID}" >/dev/null 2>&1 || true; fi
    if docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "${PG_CONTAINER}"; then
        docker exec "${PG_CONTAINER}" psql -U "${PG_USER}" -d postgres \
            -c "DROP DATABASE IF EXISTS \"${SCRATCH_DB}\"" >/dev/null 2>&1 || true
    fi
    # Remove the scratch user with retries, then force it, then the directory
    # last so nothing can re-create files under it.
    if id -u "${AGENT_USER}" >/dev/null 2>&1; then
        for _ in 1 2 3; do
            userdel "${AGENT_USER}" >/dev/null 2>&1 && break
            sleep 1
        done
        userdel -f "${AGENT_USER}" >/dev/null 2>&1 || true
    fi
    rm -rf "${SCRATCH}"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------
log "building the control plane and agents (${GOTHAM_GO}, ${ARCH})"
mkdir -p "${SCRATCH}/bin"
(
    cd "${REPO_DIR}" || exit 1
    # The release trust anchor is embedded in BOTH binaries (the dev env
    # override is deliberately not used).
    "${GOTHAM_GO}" build -o "${SCRATCH}/bin/signer" ./cmd/signer
) || { echo "build signer failed" >&2; exit 1; }

# Generate the release keypair first; keygen prints the exact ldflags value.
KEYGEN_OUT=$("${SCRATCH}/bin/signer" keygen -out "${SCRATCH}/signing.key") || {
    echo "signer keygen failed" >&2
    exit 1
}
PUBKEY=$(printf '%s' "${KEYGEN_OUT}" | sed -n 's/.*updatecore\.PublicKey=\([^"]*\).*/\1/p' | tr -d '\r\n')
if [ -z "${PUBKEY}" ]; then
    echo "could not extract the embedded public key from signer keygen output" >&2
    exit 1
fi

build_binary() { # $1 output, $2 main.version, $3 package
    (
        cd "${REPO_DIR}" || exit 1
        "${GOTHAM_GO}" build -trimpath -o "$1" \
            -ldflags "-X main.version=$2 -X github.com/justindeelux/gotham/updatecore.PublicKey=${PUBKEY}" \
            "$3"
    )
}
build_binary "${SCRATCH}/bin/gotham" "v2.0.0" ./cmd/gotham || { echo "build gotham failed" >&2; exit 1; }
build_binary "${SCRATCH}/bin/gotham-agent-v1" "v1.0.0" ./cmd/gotham-agent || { echo "build agent v1 failed" >&2; exit 1; }
build_binary "${SCRATCH}/bin/gotham-agent-v2" "v2.0.0" ./cmd/gotham-agent || { echo "build agent v2 failed" >&2; exit 1; }

# A validly signed but broken release for NEG2: the wrapper restarts the unit,
# the binary exits immediately, the health check fails and the wrapper rolls
# back.
printf '#!/bin/sh\nexit 1\n' >"${SCRATCH}/bin/bad-agent"
chmod 0755 "${SCRATCH}/bin/bad-agent"

# ---------------------------------------------------------------------------
# Release server (loopback) and signed release
# ---------------------------------------------------------------------------
log "preparing the signed agent release and the loopback release server"
RELEASE_DIR="${SCRATCH}/release"
mkdir -p "${RELEASE_DIR}/repos/verify/verify"

write_release_json() { # $1 version
    cat >"${RELEASE_DIR}/repos/verify/verify/releases" <<EOF
[{"tag_name":"$1","name":"$1","body":"verify","draft":false,"prerelease":false,"published_at":"2026-01-02T15:04:05Z","assets":[
{"name":"gotham-agent-linux-${ARCH}","browser_download_url":"http://127.0.0.1:${RELEASE_PORT}/gotham-agent-linux-${ARCH}","size":$(wc -c <"${RELEASE_DIR}/gotham-agent-linux-${ARCH}" | tr -d ' ')},
{"name":"gotham-agent-manifest-${ARCH}.txt","browser_download_url":"http://127.0.0.1:${RELEASE_PORT}/gotham-agent-manifest-${ARCH}.txt","size":$(wc -c <"${RELEASE_DIR}/gotham-agent-manifest-${ARCH}.txt" | tr -d ' ')},
{"name":"gotham-agent-manifest-${ARCH}.txt.sig","browser_download_url":"http://127.0.0.1:${RELEASE_PORT}/gotham-agent-manifest-${ARCH}.txt.sig","size":$(wc -c <"${RELEASE_DIR}/gotham-agent-manifest-${ARCH}.txt.sig" | tr -d ' ')}
]}]
EOF
}

sign_release() { # $1 version, $2 asset source path
    cp "$2" "${RELEASE_DIR}/gotham-agent-linux-${ARCH}"
    "${SCRATCH}/bin/signer" manifest -key "${SCRATCH}/signing.key" \
        -in "${RELEASE_DIR}/gotham-agent-linux-${ARCH}" \
        -version "$1" -channel stable -arch "${ARCH}" \
        -out "${RELEASE_DIR}/gotham-agent-manifest-${ARCH}.txt" >/dev/null || return 1
    write_release_json "$1"
}

sign_release "v2.0.0" "${SCRATCH}/bin/gotham-agent-v2" || { echo "sign v2 release failed" >&2; exit 1; }

python3 -m http.server "${RELEASE_PORT}" --bind 127.0.0.1 --directory "${RELEASE_DIR}" \
    >"${SCRATCH}/release.log" 2>&1 &
RELEASE_PID=$!
RELEASE_UP=0
deadline=$(( $(date +%s) + 15 ))
while [ "$(date +%s)" -lt "${deadline}" ]; do
    if curl -fsS --max-time 2 "http://127.0.0.1:${RELEASE_PORT}/repos/verify/verify/releases" >/dev/null 2>&1; then
        RELEASE_UP=1
        break
    fi
    sleep 1
done
if [ "${RELEASE_UP}" -ne 1 ]; then
    echo "the loopback release server did not come up" >&2
    cat "${SCRATCH}/release.log" >&2 || true
    exit 1
fi

# ---------------------------------------------------------------------------
# Scratch database
# ---------------------------------------------------------------------------
log "creating scratch database ${SCRATCH_DB}"
if ! docker ps --format '{{.Names}}' 2>/dev/null | grep -qx "${PG_CONTAINER}"; then
    echo "verify-agent-update.sh: Postgres container ${PG_CONTAINER} is not running" >&2
    exit 1
fi
docker exec "${PG_CONTAINER}" psql -U "${PG_USER}" -d postgres \
    -c "DROP DATABASE IF EXISTS \"${SCRATCH_DB}\"" >/dev/null 2>&1 || true
docker exec "${PG_CONTAINER}" psql -U "${PG_USER}" -d postgres \
    -c "CREATE DATABASE \"${SCRATCH_DB}\"" >/dev/null || { echo "createdb failed" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Scratch control plane
# ---------------------------------------------------------------------------
log "starting the scratch control plane (http ${HTTP_PORT}, grpc ${GRPC_PORT})"
mkdir -p "${SCRATCH}/cp"
(
    cd "${SCRATCH}/cp" || exit 1
    GOTHAM_DATABASE_DSN="${SCRATCH_DSN}" \
        "${SCRATCH}/bin/gotham" migrate up >/dev/null
) || { echo "migrations failed" >&2; exit 1; }

(
    cd "${SCRATCH}/cp" || exit 1
    GOTHAM_SERVER_ADDR=127.0.0.1 \
    GOTHAM_SERVER_PORT="${HTTP_PORT}" \
    GOTHAM_GRPC_ADDR="127.0.0.1:${GRPC_PORT}" \
    GOTHAM_DATABASE_DSN="${SCRATCH_DSN}" \
    GOTHAM_REDIS_ADDR="127.0.0.1:6379" \
    GOTHAM_SECRET_KEY="verify-secret-${RUN_ID}" \
    GOTHAM_CA_DIR="${SCRATCH}/ca" \
    GOTHAM_LOG_LEVEL=info \
    FEATURE_UPDATES=true \
    GOTHAM_UPDATE_BASE_URL="http://127.0.0.1:${RELEASE_PORT}" \
    GOTHAM_UPDATE_REPO="verify/verify" \
    GOTHAM_UPDATE_CHANNEL=stable \
    GOTHAM_UPDATE_OFFER_CACHE_TTL=3s \
    GOTHAM_UPDATE_BINARY="${SCRATCH}/cp/bin/gotham" \
    GOTHAM_UPDATE_STATUS="${SCRATCH}/cp/status/update.status" \
    GOTHAM_UPDATE_PENDING="${SCRATCH}/cp/update.pending" \
    GOTHAM_UPDATE_LOCK="${SCRATCH}/cp/update.lock" \
    GOTHAM_UPDATE_SCRIPT="${SCRATCH}/cp/gotham-update" \
    PLATFORM_ADMINS="verify@example.com" \
    exec "${SCRATCH}/bin/gotham" serve
) >"${SCRATCH}/cp.log" 2>&1 &
CP_PID=$!

deadline=$(( $(date +%s) + 30 ))
CP_UP=0
while [ "$(date +%s)" -lt "${deadline}" ]; do
    # Wait on curl's exit status and a real status code. A failing curl prints
    # "000" via -w AND would append another "000" from an `|| echo`, which made
    # the old check break on the first failed probe (the box repro).
    if code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 \
        "http://127.0.0.1:${HTTP_PORT}/healthz" 2>/dev/null) \
        && [ -n "${code}" ] && [ "${code}" != "000" ]; then
        CP_UP=1
        break
    fi
    sleep 1
done
if [ "${CP_UP}" -ne 1 ]; then
    echo "the control plane did not start listening" >&2
    print_debug_paths
    exit 1
fi

# ---------------------------------------------------------------------------
# Operator session (supported means: register, then PLATFORM_ADMINS)
# ---------------------------------------------------------------------------
log "registering the operator session"

# register_operator retries with a short backoff. It prints the HTTP status and
# body on each failed attempt so a box failure is diagnosable.
register_operator() {
    attempt=1
    while [ "${attempt}" -le 5 ]; do
        response=$(curl -sS --max-time 5 -w '\n%{http_code}' \
            -X POST -H 'Content-Type: application/json' \
            -d '{"email":"verify@example.com","password":"verify-password-123"}' \
            "http://127.0.0.1:${HTTP_PORT}/api/v1/auth/register" 2>/dev/null) || response=""
        status=$(printf '%s' "${response}" | tail -n 1)
        body=$(printf '%s' "${response}" | sed '$d')
        if [ "${status}" = "200" ]; then
            token=$(printf '%s' "${body}" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("access_token",""))' 2>/dev/null)
            if [ -n "${token}" ]; then
                TOKEN="${token}"
                return 0
            fi
        fi
        echo "registration attempt ${attempt}: status=${status:-<no response>} body=${body}" >&2
        attempt=$((attempt + 1))
        sleep 2
    done
    return 1
}

if ! register_operator; then
    echo "operator registration failed after 5 attempts" >&2
    print_debug_paths
    fail "operator registration failed"
    exit 1
fi

api_get() { curl -fsS --max-time 5 -H "Authorization: Bearer ${TOKEN}" "http://127.0.0.1:${HTTP_PORT}$1"; }
node_version() { # $1 node id; empty output on any probe failure
    api_get /api/v1/servers/agents 2>/dev/null | python3 -c '
import sys, json
want = sys.argv[1]
data = json.load(sys.stdin)
for agent in data.get("agents", []):
    if agent.get("node_id") == want:
        print(agent.get("version", ""))
        break
else:
    print("")
' "$1" 2>/dev/null
}

node_heartbeat_epoch() { # $1 node id; epoch seconds of its last heartbeat, or 0
    raw=$(api_get /api/v1/servers/agents 2>/dev/null | python3 -c '
import sys, json
want = sys.argv[1]
data = json.load(sys.stdin)
for agent in data.get("agents", []):
    if agent.get("node_id") == want:
        print(agent.get("at", ""))
        break
' "$1" 2>/dev/null)
    timestamp_to_epoch "${raw}"
}

unit_is_active() { # $1 unit; "active" when systemd reports it running
    systemctl is-active "$1" 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Scratch agents (systemd units + root-owned wrapper + sudoers)
# ---------------------------------------------------------------------------
log "installing the agent update wrapper, config, sudoers and units"
if ! id -u "${AGENT_USER}" >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin "${AGENT_USER}"
fi
DOCKER_GROUP_LINE=""
if getent group docker >/dev/null 2>&1; then
    usermod -aG docker "${AGENT_USER}"
    DOCKER_GROUP_LINE="SupplementaryGroups=docker"
fi

install_agent() { # $1 suffix (a|b), $2 node id, $3 state dir, $4 health port, $5 listen port, $6 unit
    suffix=$1; node=$2; state=$3; health=$4; listen=$5; unit=$6
    mkdir -p "${state}/bin" "${SCRATCH}/wrapper-${suffix}" "${SCRATCH}/status-${suffix}"
    cp "${SCRATCH}/bin/gotham-agent-v1" "${state}/bin/gotham-agent"
    chmod 0755 "${state}/bin/gotham-agent"
    chown "${AGENT_USER}:${AGENT_USER}" "${state}" "${state}/bin" "${state}/bin/gotham-agent"
    chown root:root "${SCRATCH}/wrapper-${suffix}" "${SCRATCH}/status-${suffix}"
    chmod 0755 "${SCRATCH}/wrapper-${suffix}" "${SCRATCH}/status-${suffix}"

    conf="${SCRATCH}/agent-updater-${suffix}.conf"
    cat >"${conf}" <<EOF
GOTHAM_BINARY=${state}/bin/gotham-agent
GOTHAM_SERVICE=${unit}
GOTHAM_HEALTH=http://127.0.0.1:${health}/healthz
GOTHAM_TIMEOUT=20
GOTHAM_STATUS=${SCRATCH}/status-${suffix}/${node}.status
GOTHAM_PENDING=${state}/update.pending
GOTHAM_LOCK=${state}/update.lock
GOTHAM_GRACE=0
EOF
    chmod 0644 "${conf}"
    chown root:root "${conf}"

    wrapper="${SCRATCH}/wrapper-${suffix}/gotham-agent-update"
    sed "s#/etc/gotham/agent-updater.conf#${conf}#g" \
        "${REPO_DIR}/deploy/gotham-update.sh" >"${wrapper}"
    chmod 0755 "${wrapper}"
    chown root:root "${wrapper}"

    cat >"${UNIT_DIR}/${unit}.service" <<EOF
[Unit]
Description=Gotham agent update verification (${suffix}, scratch)
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
User=${AGENT_USER}
Group=${AGENT_USER}
WorkingDirectory=${state}
Environment=GOTHAM_AGENT_CP_ADDR=127.0.0.1:${GRPC_PORT}
Environment=GOTHAM_AGENT_NODE_ID=${node}
Environment=GOTHAM_AGENT_LISTEN_ADDR=127.0.0.1:${listen}
Environment=GOTHAM_AGENT_CERT_DIR=${state}
Environment=GOTHAM_AGENT_BINARY=${state}/bin/gotham-agent
Environment=GOTHAM_AGENT_UPDATE_SCRIPT=${wrapper}
Environment=GOTHAM_AGENT_UPDATE_STATUS=${SCRATCH}/status-${suffix}/${node}.status
Environment=GOTHAM_AGENT_UPDATE_PENDING=${state}/update.pending
Environment=GOTHAM_AGENT_UPDATE_LOCK=${state}/update.lock
Environment=GOTHAM_AGENT_UPDATE_RETRY=${state}/update.retry
Environment=GOTHAM_AGENT_UPDATE_BACKOFF=${state}/update.backoff
Environment=GOTHAM_AGENT_HEALTH_ADDR=127.0.0.1:${health}
Environment=GOTHAM_AGENT_UPDATE_INTERVAL=5s
Environment=GOTHAM_AGENT_AUTO_UPDATE=false
Environment=GOTHAM_AGENT_LOG_LEVEL=debug
Environment=GOTHAM_AGENT_DOCKER_SOCK=/var/run/docker.sock
${DOCKER_GROUP_LINE}
ExecStart=${state}/bin/gotham-agent serve
Restart=always
RestartSec=2
KillMode=process
TimeoutStopSec=10
EOF
    chmod 0644 "${UNIT_DIR}/${unit}.service"
    echo "${wrapper}"
}

WRAPPER_A=$(install_agent a "${NODE_A}" "${STATE_A}" "${HEALTH_A_PORT}" "${AGENT_LISTEN_A}" "${UNIT_A}")
WRAPPER_B=$(install_agent b "${NODE_B}" "${STATE_B}" "${HEALTH_B_PORT}" "${AGENT_LISTEN_B}" "${UNIT_B}")

cat >"${SUDOERS_FILE}" <<EOF
Defaults:${AGENT_USER} !requiretty
${AGENT_USER} ALL=(root) NOPASSWD: ${WRAPPER_A} ""
${AGENT_USER} ALL=(root) NOPASSWD: ${WRAPPER_B} ""
EOF
chmod 0440 "${SUDOERS_FILE}"
if command -v visudo >/dev/null 2>&1; then
    visudo -cf "${SUDOERS_FILE}" >/dev/null 2>&1 || { echo "scratch sudoers is invalid" >&2; exit 1; }
fi

systemctl daemon-reload
systemctl start "${UNIT_A}" "${UNIT_B}"

# ---------------------------------------------------------------------------
# C1/C3 baseline: both agents report v1.0.0
# ---------------------------------------------------------------------------
log "waiting for both agents to report v1.0.0"
wait_version() { # $1 node, $2 version, $3 timeout
    _deadline=$(( $(date +%s) + $3 ))
    while [ "$(date +%s)" -lt "${_deadline}" ]; do
        [ "$(node_version "$1")" = "$2" ] && return 0
        sleep 1
    done
    return 1
}

if wait_version "${NODE_A}" "v1.0.0" 60 && wait_version "${NODE_B}" "v1.0.0" 60; then
    pass "C1-baseline both agents registered and report v1.0.0"
else
    fail "agents did not report v1.0.0 (a=$(node_version "${NODE_A}") b=$(node_version "${NODE_B}"))"
    echo "--- agent A journal ---" >&2; journalctl -u "${UNIT_A}" -n 40 --no-pager >&2 || true
    echo "--- agent B journal ---" >&2; journalctl -u "${UNIT_B}" -n 40 --no-pager >&2 || true
    print_debug_paths
    exit 1
fi

# Snapshot the planted install directories (md5 + mtime), excluding exactly the
# files an update is allowed to change.
snapshot() {
    (
        cd "${SCRATCH}/agents" || exit 1
        find . -type f \
            ! -path './*/bin/gotham-agent' \
            ! -path './*/bin/gotham-agent.old' \
            ! -name 'update.pending' \
            ! -name 'update.lock' \
            ! -name '*.status' \
            | LC_ALL=C sort | while IFS= read -r f; do
                printf '%s %s %s\n' "$f" "$(md5sum "$f" | cut -d' ' -f1)" "$(stat -c '%Y' "$f")"
            done
    )
}
snapshot >"${SCRATCH}/snapshot.before"

# ---------------------------------------------------------------------------
# C1: the CP is already on the newest release; update-all must still roll out
# ---------------------------------------------------------------------------
log "checking the control plane's own version and the agent target"
CP_VERSION=$("${SCRATCH}/bin/gotham" version 2>/dev/null | awk '{print $2}')
LATEST=$(api_get /api/v1/servers/agents | python3 -c 'import sys,json;print(json.load(sys.stdin).get("latest_version",""))' 2>/dev/null)
if [ "${CP_VERSION}" = "v2.0.0" ] && [ "${LATEST}" = "v2.0.0" ]; then
    pass "C4 control plane is v2.0.0 (current) and the agent target resolves to v2.0.0"
else
    fail "C4 control plane version=${CP_VERSION} agent target=${LATEST}, want v2.0.0/v2.0.0"
fi

log "triggering update-all (control plane already current)"
UPDATE_ALL=$(curl -fsS -X POST -H "Authorization: Bearer ${TOKEN}" \
    "http://127.0.0.1:${HTTP_PORT}/api/v1/servers/agents/update-all" 2>/dev/null) || UPDATE_ALL=""
TARGET=$(printf '%s' "${UPDATE_ALL}" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("target_version",""))' 2>/dev/null)
PENDING=$(printf '%s' "${UPDATE_ALL}" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("pending",""))' 2>/dev/null)
if [ "${TARGET}" = "v2.0.0" ] && [ "${PENDING}" = "2" ]; then
    pass "C1 update-all with the CP current starts a rollout (target v2.0.0, pending 2)"
else
    fail "C1 update-all = ${UPDATE_ALL:-<error>}, want target v2.0.0 pending 2"
fi

# ---------------------------------------------------------------------------
# C2/C3: both agents converge on v2.0.0 through the wrapper
# ---------------------------------------------------------------------------
log "waiting for both agents to converge on v2.0.0"
CONVERGED=1
wait_version "${NODE_A}" "v2.0.0" 120 || CONVERGED=0
wait_version "${NODE_B}" "v2.0.0" 120 || CONVERGED=0
if [ "${CONVERGED}" -eq 1 ]; then
    pass "C3 both heartbeats converge on v2.0.0"
else
    fail "C3 agents did not converge (a=$(node_version "${NODE_A}") b=$(node_version "${NODE_B}"))"
fi

for suffix in a b; do
    node="${NODE_A}"; [ "${suffix}" = "b" ] && node="${NODE_B}"
    status="${SCRATCH}/status-${suffix}/${node}.status"
    if [ -f "${status}" ] && grep -q '^result=ok' "${status}"; then
        pass "C2 agent ${suffix} wrapper status records ok"
    else
        fail "C2 agent ${suffix} wrapper status is not ok: $(cat "${status}" 2>/dev/null || echo missing)"
    fi
    backup="${SCRATCH}/agents/${suffix}/bin/gotham-agent.old"
    if [ -f "${backup}" ]; then
        pass "C2 agent ${suffix} kept the previous binary as gotham-agent.old"
    else
        fail "C2 agent ${suffix} did not retain gotham-agent.old"
    fi
done

# C3: everything else under the planted install dirs is byte-identical.
snapshot >"${SCRATCH}/snapshot.after"
if diff -u "${SCRATCH}/snapshot.before" "${SCRATCH}/snapshot.after" >"${SCRATCH}/snapshot.diff" 2>&1; then
    pass "C3 planted install is otherwise byte-identical (md5+mtime, swap/backup/status/marker excluded)"
else
    fail "C3 planted install changed beyond the intended files:"
    sed -n '1,40p' "${SCRATCH}/snapshot.diff" >&2
fi

# ---------------------------------------------------------------------------
# NEG1: a tampered asset (digest mismatch) leaves the agents on v2.0.0
# ---------------------------------------------------------------------------
log "NEG1 tampered asset: signing v3.0.0 but serving a different binary"
build_binary "${SCRATCH}/bin/gotham-agent-v3" "v3.0.0" ./cmd/gotham-agent || { echo "build agent v3 failed" >&2; exit 1; }
sign_release "v3.0.0" "${SCRATCH}/bin/gotham-agent-v3" || fail "NEG1 could not sign the v3 release"
# Serve the v2 binary under the v3 manifest's digest.
cp "${SCRATCH}/bin/gotham-agent-v2" "${RELEASE_DIR}/gotham-agent-linux-${ARCH}"
write_release_json "v3.0.0"
sleep 5 # let the CP's 3s offer cache lapse

curl -fsS -X POST -H "Authorization: Bearer ${TOKEN}" \
    "http://127.0.0.1:${HTTP_PORT}/api/v1/servers/agents/update-all" >/dev/null 2>&1 || true
sleep 15
NEG1_A=$(node_version "${NODE_A}")
NEG1_B=$(node_version "${NODE_B}")
if [ "${NEG1_A}" = "v2.0.0" ] && [ "${NEG1_B}" = "v2.0.0" ]; then
    pass "NEG1 tampered asset refused; agents stay on v2.0.0"
else
    fail "NEG1 agents moved to v3.0.0 despite a tampered asset (a=${NEG1_A} b=${NEG1_B})"
fi

# ---------------------------------------------------------------------------
# NEG2: a validly signed but broken release rolls back and records rolled_back
# ---------------------------------------------------------------------------
log "NEG2 broken release: signing v4.0.0 over a binary that fails to start"
sign_release "v4.0.0" "${SCRATCH}/bin/bad-agent" || fail "NEG2 could not sign the v4 release"
sleep 5 # let the offer cache lapse

curl -fsS -X POST -H "Authorization: Bearer ${TOKEN}" \
    "http://127.0.0.1:${HTTP_PORT}/api/v1/servers/agents/update-all" >/dev/null 2>&1 || true

# Wait until BOTH wrapper status files record rolled_back.
ROLLED_A=0
ROLLED_B=0
deadline=$(( $(date +%s) + 120 ))
while [ "$(date +%s)" -lt "${deadline}" ]; do
    [ -f "${SCRATCH}/status-a/${NODE_A}.status" ] && grep -q '^result=rolled_back' "${SCRATCH}/status-a/${NODE_A}.status" && ROLLED_A=1
    [ -f "${SCRATCH}/status-b/${NODE_B}.status" ] && grep -q '^result=rolled_back' "${SCRATCH}/status-b/${NODE_B}.status" && ROLLED_B=1
    [ "${ROLLED_A}" -eq 1 ] && [ "${ROLLED_B}" -eq 1 ] && break
    sleep 2
done
if [ "${ROLLED_A}" -eq 1 ] && [ "${ROLLED_B}" -eq 1 ]; then
    pass "NEG2 both wrapper statuses record rolled_back"
else
    fail "NEG2 rollback not recorded (a=${ROLLED_A} b=${ROLLED_B})"
fi

# A rolled_back status alone is not enough: the old check read the CP's version
# map, which stayed v2 even while systemd's start rate limit left the units
# dead. Assert both units are actually running the restored binary.
HEARTBEAT_TS=$(date +%s)
ACTIVE_OK=1
for suffix in a b; do
    unit="${UNIT_A}"; [ "${suffix}" = "b" ] && unit="${UNIT_B}"
    state=""
    deadline=$(( $(date +%s) + 30 ))
    while [ "$(date +%s)" -lt "${deadline}" ]; do
        state=$(unit_is_active "${unit}")
        [ "${state}" = "active" ] && break
        sleep 1
    done
    if [ "${state}" = "active" ]; then
        pass "NEG2 agent ${suffix} unit is active after rollback"
    else
        fail "NEG2 agent ${suffix} unit is not active after rollback (state=${state:-unknown})"
        ACTIVE_OK=0
    fi
done

# A fresh heartbeat after the rollback proves the restored binary really runs.
HEARTBEAT_OK=1
for suffix in a b; do
    node="${NODE_A}"; [ "${suffix}" = "b" ] && node="${NODE_B}"
    beat=0
    deadline=$(( $(date +%s) + 30 ))
    while [ "$(date +%s)" -lt "${deadline}" ]; do
        at=$(node_heartbeat_epoch "${node}")
        if [ -n "${at}" ] && [ "${at}" -ge "${HEARTBEAT_TS}" ] 2>/dev/null; then
            beat=1
            break
        fi
        sleep 2
    done
    if [ "${beat}" -eq 1 ]; then
        pass "NEG2 agent ${suffix} sent a fresh heartbeat after the rollback"
    else
        fail "NEG2 agent ${suffix} sent no fresh heartbeat after the rollback"
        HEARTBEAT_OK=0
    fi
done

# The failed release must not be re-applied in a loop: watch a quiet window and
# require the wrapper status timestamps and the systemd restart counters to stay
# put (the agent seeds a durable-status backoff so it skips the bad release).
status_at() { # $1 status file -> the recorded epoch, or ""
    [ -f "$1" ] || { echo ""; return; }
    sed -n 's/^at=//p' "$1" 2>/dev/null | head -n 1
}
unit_nrestarts() { systemctl show -p NRestarts --value "$1" 2>/dev/null || echo ""; }

STATUS_A="${SCRATCH}/status-a/${NODE_A}.status"
STATUS_B="${SCRATCH}/status-b/${NODE_B}.status"
at_a_before=$(status_at "${STATUS_A}")
at_b_before=$(status_at "${STATUS_B}")
restarts_a_before=$(unit_nrestarts "${UNIT_A}")
restarts_b_before=$(unit_nrestarts "${UNIT_B}")
log "NEG2 watching a 30s quiet window for a crash-loop"
sleep 30
at_a_after=$(status_at "${STATUS_A}")
at_b_after=$(status_at "${STATUS_B}")
restarts_a_after=$(unit_nrestarts "${UNIT_A}")
restarts_b_after=$(unit_nrestarts "${UNIT_B}")

LOOP_OK=1
if [ -n "${at_a_before}" ] && [ "${at_a_before}" = "${at_a_after}" ] \
    && [ -n "${at_b_before}" ] && [ "${at_b_before}" = "${at_b_after}" ]; then
    pass "NEG2 no re-apply during a 30s window (wrapper status timestamps unchanged)"
else
    fail "NEG2 the failed release was re-applied (status at a=${at_a_before}->${at_a_after} b=${at_b_before}->${at_b_after})"
    LOOP_OK=0
fi
if [ -z "${restarts_a_before}" ] || [ -z "${restarts_b_before}" ] \
    || { [ "${restarts_a_before}" = "${restarts_a_after}" ] && [ "${restarts_b_before}" = "${restarts_b_after}" ]; }; then
    pass "NEG2 no further systemd restarts during the window"
else
    fail "NEG2 systemd restarts increased (a=${restarts_a_before}->${restarts_a_after} b=${restarts_b_before}->${restarts_b_after})"
    LOOP_OK=0
fi

# Now stop the units so a fresh process cannot retry the bad release in a loop.
systemctl stop "${UNIT_A}" "${UNIT_B}" >/dev/null 2>&1 || true
NEG2_A=$(node_version "${NODE_A}")
NEG2_B=$(node_version "${NODE_B}")
if [ "${ROLLED_A}" -eq 1 ] && [ "${ROLLED_B}" -eq 1 ] && [ "${ACTIVE_OK}" -eq 1 ] && [ "${HEARTBEAT_OK}" -eq 1 ] \
    && [ "${LOOP_OK}" -eq 1 ] && [ "${NEG2_A}" = "v2.0.0" ] && [ "${NEG2_B}" = "v2.0.0" ]; then
    pass "NEG2 broken signed release rolled back, both units active, agents on v2.0.0"
else
    fail "NEG2 broken release not rolled back cleanly (rolled=${ROLLED_A}/${ROLLED_B} active=${ACTIVE_OK} heartbeat=${HEARTBEAT_OK} loop=${LOOP_OK} a=${NEG2_A} b=${NEG2_B})"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo
if [ "${VERIFY_KEEP}" = "1" ]; then
    echo "VERIFY_KEEP=1: scratch artifacts kept at ${SCRATCH}"
    print_debug_paths
else
    echo "Scratch artifacts: ${SCRATCH} (removed on exit; set VERIFY_KEEP=1 to keep them)"
fi
if [ "${FAILURES}" -eq 0 ]; then
    echo "verify-agent-update: all checks passed"
    exit 0
fi
echo "verify-agent-update: ${FAILURES} check(s) failed" >&2
exit 1
