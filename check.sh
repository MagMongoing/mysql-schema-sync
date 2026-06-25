#!/bin/bash
# Enable tracing only when DEBUG is set (avoids leaking config content to logs)
[ -n "$DEBUG" ] && set -x
cd "$(dirname "$0")"
if [ ! -d "log" ]; then
   mkdir -p log
fi

exec 1>>"log/sync.log.$(date +"%Y%m%d")" 2>&1

shopt -s nullglob
exit_code=0
for f in *.json
do
    # Skip files that don't look like valid config (must have "source" or "dest" keys)
    if ! grep -Eq '"source"|"dest"' "$f" 2>/dev/null; then
        echo "SKIP: $f does not look like a valid config"
        continue
    fi
    if ! ./mysql-schema-sync -conf "$f" -sync; then
        echo "FAIL: $f sync failed with exit code $?"
        exit_code=1
    fi
done
shopt -u nullglob

DAY_MAX=15
find log/ -type f -name "*.log*" -mtime +"$DAY_MAX" -delete 2>/dev/null

exit $exit_code
