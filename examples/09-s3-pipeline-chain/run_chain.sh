#!/usr/bin/env bash
# =============================================================================
# run_chain.sh — Цепочка двух пайплайнов: извлечение → разделение по региону
# =============================================================================
# Шаг 1. tdtpcli --pipeline pipeline_0_get_regions.yaml
#         PostgreSQL → /tmp/tdtp_regions.xml  (список уникальных регионов)
#
# Шаг 2. tdtpcli --pipeline pipeline_1_extract.yaml
#         PostgreSQL orders+users → S3 my-bucket/pipeline/orders_full.tdtp.xml
#
# Шаг 3. Для каждого региона из /tmp/tdtp_regions.xml:
#         tdtpcli --pipeline /tmp/p2_<REGION>.yaml
#         S3 orders_full.tdtp.xml → S3 by_region/<REGION>/orders.tdtp.xml
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="$SCRIPT_DIR/pipeline_2_split_template.yaml"
TDTP="${TDTP_CLI:-tdtpcli}"

log() { echo "[$(date '+%H:%M:%S')] $*"; }
die() { echo "ERROR: $*" >&2; exit 1; }

command -v "$TDTP" >/dev/null 2>&1 || die "tdtpcli not found (set TDTP_CLI=...)"

# ============================================================================
# ШАГ 1 — Получаем список регионов через tdtpcli
# ============================================================================
log "=== STEP 1: Get distinct regions via tdtpcli ==="
"$TDTP" --pipeline "$SCRIPT_DIR/pipeline_0_get_regions.yaml"

# Извлекаем значения из TDTP XML: каждая строка — <R>ЗНАЧЕНИЕ</R>
REGIONS=$(grep '<R>' /tmp/tdtp_regions.xml | sed 's|.*<R>\(.*\)</R>.*|\1|')
log "Regions found: $(echo "$REGIONS" | tr '\n' ' ')"

# ============================================================================
# ШАГ 2 — Полный экспорт в S3
# ============================================================================
log "=== STEP 2: Extract all orders to S3 ==="
"$TDTP" --pipeline "$SCRIPT_DIR/pipeline_1_extract.yaml"
log "Step 2 complete."

# ============================================================================
# ШАГ 3 — Разделение по регионам
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
log "    my-bucket/pipeline/orders_full.tdtp.xml   — полный файл"
log "    my-bucket/pipeline/by_region/NORTH/orders.tdtp.xml"
log "    my-bucket/pipeline/by_region/SOUTH/orders.tdtp.xml"
log "    my-bucket/pipeline/by_region/EAST/orders.tdtp.xml"
log "    my-bucket/pipeline/by_region/WEST/orders.tdtp.xml"
