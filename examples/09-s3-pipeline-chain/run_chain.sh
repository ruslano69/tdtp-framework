#!/usr/bin/env bash
# =============================================================================
# run_chain.sh — Цепочка двух пайплайнов: извлечение → разделение по региону
# =============================================================================
# Шаг 1. tdtpcli --pipeline pipeline_1_extract.yaml
#         PostgreSQL orders+users → S3 my-bucket/pipeline/orders_full.tdtp.xml
#
# Шаг 2. Для каждого уникального региона:
#         tdtpcli --pipeline /tmp/p2_<REGION>.yaml
#         S3 orders_full.tdtp.xml → S3 by_region/<REGION>/orders.tdtp.xml
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="$SCRIPT_DIR/pipeline_2_split_template.yaml"
TDTP="${TDTP_CLI:-tdtpcli}"          # путь к бинарю, можно переопределить

# Параметры PostgreSQL (для получения списка регионов)
PG_DSN="postgres://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test"

# ---------------------------------------------------------------------------
log() { echo "[$(date '+%H:%M:%S')] $*"; }
die() { echo "ERROR: $*" >&2; exit 1; }

command -v "$TDTP" >/dev/null 2>&1 || die "tdtpcli not found (set TDTP_CLI=...)"
# ---------------------------------------------------------------------------

# ============================================================================
# ШАГ 1 — Полный экспорт в S3
# ============================================================================
log "=== STEP 1: Extract orders to S3 ==="
"$TDTP" --pipeline "$SCRIPT_DIR/pipeline_1_extract.yaml"
log "Step 1 complete."

# ============================================================================
# ШАГ 2 — Получаем список регионов
# ============================================================================
log "=== STEP 2: Fetching distinct regions from DB ==="

# Вариант A: запросить из PostgreSQL напрямую (требует psql)
if command -v psql >/dev/null 2>&1; then
    REGIONS=$(psql "$PG_DSN?sslmode=disable" -t -A -c "
        SELECT DISTINCT
          CASE (REPLACE(u.username, 'user_', '')::int % 4)::text
            WHEN '0' THEN 'NORTH'
            WHEN '1' THEN 'SOUTH'
            WHEN '2' THEN 'EAST'
            ELSE 'WEST'
          END AS region
        FROM orders o JOIN users u ON o.user_id = u.id
        ORDER BY 1
    ")
else
    # Вариант B: задать список вручную (замените на свои значения)
    log "psql not found — using hardcoded region list"
    REGIONS="EAST
NORTH
SOUTH
WEST"
fi

log "Regions found: $(echo "$REGIONS" | tr '\n' ' ')"

# ============================================================================
# ШАГ 3 — Запускаем pipeline 2 для каждого региона
# ============================================================================
log "=== STEP 3: Split by region ==="

TMPDIR_CHAIN="${TMPDIR:-/tmp}/tdtp_chain_$$"
mkdir -p "$TMPDIR_CHAIN"
trap 'rm -rf "$TMPDIR_CHAIN"' EXIT

SUCCESS=0
FAIL=0

while IFS= read -r REGION; do
    [[ -z "$REGION" ]] && continue

    log "--- Region: $REGION ---"
    TMP_YAML="$TMPDIR_CHAIN/pipeline_2_${REGION}.yaml"

    # Подставляем регион в шаблон
    sed "s/__REGION__/${REGION}/g" "$TEMPLATE" > "$TMP_YAML"

    if "$TDTP" --pipeline "$TMP_YAML"; then
        log "  OK: $REGION"
        SUCCESS=$((SUCCESS + 1))
    else
        log "  FAILED: $REGION" >&2
        FAIL=$((FAIL + 1))
    fi
done <<< "$REGIONS"

# ============================================================================
# ИТОГ
# ============================================================================
log "=== DONE ==="
log "  Success: $SUCCESS region(s)"
[[ $FAIL -gt 0 ]] && log "  Failed:  $FAIL region(s)" && exit 1
log "  S3 layout:"
log "    my-bucket/pipeline/orders_full.tdtp.xml   — исходный полный файл"
log "    my-bucket/pipeline/by_region/NORTH/orders.tdtp.xml"
log "    my-bucket/pipeline/by_region/SOUTH/orders.tdtp.xml"
log "    my-bucket/pipeline/by_region/EAST/orders.tdtp.xml"
log "    my-bucket/pipeline/by_region/WEST/orders.tdtp.xml"
