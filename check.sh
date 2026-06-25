#!/bin/bash
# Requires bash 4+ (for shopt -s nullglob).
# Enable tracing only when DEBUG is set (avoids leaking config content to logs)
[ -n "${DEBUG:-}" ] && set -x

# -u: error on unset variables (use ${VAR:-default} for optional vars).
# pipefail: surface errors from any command in a pipeline.
# We deliberately do NOT enable -e because the per-config loop depends on
# continuing past failures (tracked via $exit_code).
set -uo pipefail

# L8: resolve symlinks to the real script directory via readlink -f / realpath.
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd -P)"
cd "$SCRIPT_DIR" || { echo "cannot cd to script directory" >&2; exit 1; }

if [ ! -d "log" ]; then
    mkdir -p log || { echo "cannot cd to log directory" >&2; exit 1; }
fi

# M20: handle exec redirect failure explicitly.
LOG_FILE="log/sync.log.$(date +"%Y%m%d")"
if ! exec 1>>"$LOG_FILE" 2>&1; then
    echo "FATAL: cannot open log file $LOG_FILE" >&2
    exit 1
fi

# M19: concurrency guard via flock to prevent overlapping cron invocations
# from running concurrent DDL against the same destination.
# H19: if flock is not available, warn instead of silently skipping the lock.
LOCK_FILE="log/.sync.lock"
if command -v flock >/dev/null 2>&1; then
    exec 9>>"$LOCK_FILE"
    if ! flock -n 9; then
        echo "another instance is already running (lock: $LOCK_FILE)" >&2
        exit 1
    fi
    # Write PID for monitoring purposes
    echo $$ >&9
else
    echo "WARN: flock not available; concurrent runs are NOT prevented" >&2
    # Fallback: use mkdir-based lock (portable across macOS/Linux)
    # L8: detect stale locks by checking if the owning process is still alive.
    if ! mkdir "$LOCK_FILE.d" 2>/dev/null; then
        if [ -f "$LOCK_FILE.d/pid" ]; then
            old_pid=$(cat "$LOCK_FILE.d/pid" 2>/dev/null || echo "")
            if [ -n "$old_pid" ] && kill -0 "$old_pid" 2>/dev/null; then
                echo "another instance (PID $old_pid) is still running" >&2
                exit 1
            fi
            echo "WARN: stale lock from PID $old_pid, removing" >&2
            rm -rf "$LOCK_FILE.d" 2>/dev/null
            if ! mkdir "$LOCK_FILE.d" 2>/dev/null; then
                echo "race: another instance grabbed lock" >&2
                exit 1
            fi
        else
            echo "another instance is already running (mkdir lock: $LOCK_FILE.d)" >&2
            exit 1
        fi
    fi
    echo $$ > "$LOCK_FILE.d/pid"
    trap 'rm -rf "$LOCK_FILE.d" 2>/dev/null' EXIT
fi

# L30: rotate log BEFORE exec redirect to avoid writing to unlinked inode.
DAY_MAX=15
# Match only the rotated log files this script produces (sync.log.YYYYMMDD)
# to avoid accidentally deleting unrelated log files some other process placed here.
find log/ -type f -name 'sync.log.*' -mtime +"$DAY_MAX" -delete

MAX_LOG_BYTES=$((50 * 1024 * 1024))
if [ -f "$LOG_FILE" ]; then
    # H18: use stat instead of wc -c for portability (macOS/BSD wc outputs leading spaces)
    if command -v stat >/dev/null 2>&1; then
        if stat -c %s "$LOG_FILE" >/dev/null 2>&1; then
            log_size=$(stat -c %s "$LOG_FILE")  # GNU stat
        else
            log_size=$(stat -f %z "$LOG_FILE" 2>/dev/null || echo 0)  # BSD stat
        fi
    else
        log_size=$(wc -c < "$LOG_FILE" 2>/dev/null | tr -d ' ' || echo 0)
    fi
    if [ "$log_size" -gt "$MAX_LOG_BYTES" ]; then
        # Truncate to last 10 MB (keep recent entries).
        tail -c $((10 * 1024 * 1024)) "$LOG_FILE" > "${LOG_FILE}.trunc"
        mv "${LOG_FILE}.trunc" "$LOG_FILE"
        # L7: re-open fd to reset file offset to new EOF (avoids sparse-file gap).
        exec 1>>"$LOG_FILE" 2>&1
    fi
fi

shopt -s nullglob
exit_code=0
for f in *.json
do
    # M18: tighter config detection — require BOTH "source" and "dest" top-level
    # keys. H20: use POSIX character classes for portability (BSD/macOS).
    # L3: strip comment lines (supported by loadJSONFile) before checking,
    # so commented-out keys don't cause false positives.
    stripped=$(grep -v '^\s*[#/]' "$f" 2>/dev/null)
    if ! echo "$stripped" | grep -Eq '"source"[[:space:]]*:' || ! echo "$stripped" | grep -Eq '"dest"[[:space:]]*:'; then
        echo "SKIP: $f does not look like a valid mysql-schema-sync config"
        continue
    fi
    # H7: capture exit code BEFORE the conditional (if ! cmd; then rc=$? always gives 0).
    ./mysql-schema-sync -conf "$f" -sync
    rc=$?
    if [ "$rc" -ne 0 ]; then
        echo "FAIL: $f sync failed with exit code $rc"
        exit_code=1
    fi
done
shopt -u nullglob

exit $exit_code
