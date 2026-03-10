#!/usr/bin/env bash
# run_tests.sh — тестирует compact-экспорт/импорт и конвертацию через tdtpcli
set -euo pipefail

CLI=/tmp/tdtpcli
CFG=/tmp/test_compact/config.yaml
DIR=/tmp/test_compact
DB=$DIR/test.db

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'

ok()   { echo -e "${GREEN}✓ $*${NC}"; }
fail() { echo -e "${RED}✗ $*${NC}"; exit 1; }
hdr()  { echo -e "\n${CYAN}═══ $* ═══${NC}"; }
info() { echo -e "${YELLOW}  $*${NC}"; }

# ── 0. Версия ──────────────────────────────────────────────────────────────────
hdr "0. Версия"
$CLI --version
echo

# ── 1. Обычный экспорт employees (baseline) ────────────────────────────────────
hdr "1. Обычный экспорт employees (v1.0, без compact)"
$CLI --export employees --output $DIR/employees_plain.xml --config $CFG
ok "Файл создан: employees_plain.xml"
grep -q 'version="1.0"' $DIR/employees_plain.xml && ok "version=1.0" || fail "version mismatch"
grep -q 'compact' $DIR/employees_plain.xml && fail "compact не должен быть в plain-файле" || ok "compact отсутствует (ожидаемо)"

# ── 2. Экспорт view с _ prefix → auto-detect fixed fields ─────────────────────
hdr "2. Экспорт VIEW dept_employees_report --compact (auto-detect _prefix)"
$CLI --export dept_employees_report --compact --output $DIR/dept_emp_compact.xml --config $CFG
ok "Файл создан: dept_emp_compact.xml"
grep -q 'compact="true"' $DIR/dept_emp_compact.xml && ok 'Data compact="true"' || fail "compact не установлен"
grep -q 'fixed="true"' $DIR/dept_emp_compact.xml       && ok 'fixed="true" есть в Schema' || fail "fixed не установлен"
grep -q 'version="1.3.1"' $DIR/dept_emp_compact.xml    && ok 'version="1.3.1"'             || fail "версия не обновлена"
# Проверяем что поля без _prefix, но fixed
grep -q 'name="_dept_id"' $DIR/dept_emp_compact.xml && fail "_dept_id должен быть переименован в dept_id" || ok "_dept_id stripped → dept_id"
grep -q 'name="dept_id"'  $DIR/dept_emp_compact.xml && ok 'dept_id в Schema' || fail "dept_id не найден"

info "Первые 5 строк Data:"
grep '<R>' $DIR/dept_emp_compact.xml | head -5

# ── 3. Проверка структуры compact-строк ───────────────────────────────────────
hdr "3. Проверка compact-структуры (пропуски в fixed полях)"
ROWS=$(grep '<R>' $DIR/dept_emp_compact.xml | wc -l)
info "Строк в Data: $ROWS (ожидается 15)"
[ "$ROWS" -eq 15 ] && ok "15 строк" || fail "Ожидалось 15 строк, получено $ROWS"

# В compact-формате строки 2+ в каждой группе начинаются с || (пропуски fixed полей)
EMPTY_FIXED=$(grep '<R>||' $DIR/dept_emp_compact.xml | wc -l)
info "Строк с пропусками fixed полей (начинаются ||): $EMPTY_FIXED (ожидается 12)"
[ "$EMPTY_FIXED" -eq 12 ] && ok "12 строк с carry-forward пропусками" || fail "Ожидалось 12, получено $EMPTY_FIXED"

# ── 4. Экспорт с явным --fixed-fields ─────────────────────────────────────────
hdr "4. Экспорт employees --compact --fixed-fields dept_id"
$CLI --export employees --compact --fixed-fields dept_id --output $DIR/emp_fixed_explicit.xml --config $CFG
ok "Файл создан: emp_fixed_explicit.xml"
grep -q 'compact="true"' $DIR/emp_fixed_explicit.xml && ok 'compact="true"' || fail "compact не установлен"
grep -q 'name="dept_id".*fixed="true"' $DIR/emp_fixed_explicit.xml && ok 'dept_id fixed=true' || fail "dept_id не помечен fixed"

# ── 5. Импорт compact-файла в новую таблицу ───────────────────────────────────
hdr "5. Импорт compact-файла в dept_emp_imported"
$CLI --import $DIR/dept_emp_compact.xml --table dept_emp_imported --strategy replace --config $CFG
ok "Импорт завершён"

# Проверяем что данные попали в таблицу
ROW_COUNT=$(python3 -c "
import sqlite3
con = sqlite3.connect('$DB')
cur = con.cursor()
cur.execute('SELECT COUNT(*) FROM dept_emp_imported')
print(cur.fetchone()[0])
con.close()
")
info "Строк в dept_emp_imported: $ROW_COUNT (ожидается 15)"
[ "$ROW_COUNT" -eq 15 ] && ok "15 строк импортировано" || fail "Ожидалось 15, получено $ROW_COUNT"

# Проверяем что данные корректны — первый сотрудник Sales
DEPT=$(python3 -c "
import sqlite3
con = sqlite3.connect('$DB')
cur = con.cursor()
cur.execute('SELECT dept_id, dept_name, location FROM dept_emp_imported WHERE emp_id=101')
row = cur.fetchone()
print(row[0], row[1], row[2])
con.close()
")
info "dept_emp_imported emp_id=101: $DEPT"
echo "$DEPT" | grep -q "10 Sales Moscow" && ok "Данные корректны (dept_id=10, Sales, Moscow)" || fail "Данные некорректны: $DEPT"

# ── 6. Конвертация plain-файла в compact ──────────────────────────────────────
hdr "6. --to-compact: конвертация employees_plain.xml → compact"
$CLI --to-compact $DIR/employees_plain.xml --output $DIR/employees_converted.xml --fixed-fields dept_id --config $CFG
ok "Файл создан: employees_converted.xml"
grep -q 'compact="true"' $DIR/employees_converted.xml  && ok 'compact="true"' || fail "compact не установлен"
grep -q 'version="1.3.1"' $DIR/employees_converted.xml && ok 'version="1.3.1"' || fail "версия не обновлена"

# ── 7. Импорт converted-файла ─────────────────────────────────────────────────
hdr "7. Импорт converted-файла в employees_from_converted"
$CLI --import $DIR/employees_converted.xml --table employees_from_converted --strategy replace --config $CFG
ok "Импорт завершён"

ROW_COUNT2=$(python3 -c "
import sqlite3
con = sqlite3.connect('$DB')
cur = con.cursor()
cur.execute('SELECT COUNT(*) FROM employees_from_converted')
print(cur.fetchone()[0])
con.close()
")
info "Строк в employees_from_converted: $ROW_COUNT2 (ожидается 15)"
[ "$ROW_COUNT2" -eq 15 ] && ok "15 строк импортировано" || fail "Ожидалось 15, получено $ROW_COUNT2"

# ── 8. Размер файлов: сравнение plain vs compact ──────────────────────────────
hdr "8. Сравнение размеров файлов"
PLAIN_SIZE=$(wc -c < $DIR/employees_plain.xml)
COMPACT_SIZE=$(wc -c < $DIR/dept_emp_compact.xml)
info "employees_plain.xml:      $PLAIN_SIZE байт"
info "dept_emp_compact.xml:     $COMPACT_SIZE байт"

# ── 9. Финальная сводка ───────────────────────────────────────────────────────
hdr "9. Итог"
echo -e "${GREEN}"
echo "  Все тесты пройдены!"
echo "  ✓ Экспорт с auto-detect _prefix → compact"
echo "  ✓ Экспорт с --fixed-fields"
echo "  ✓ Импорт compact (auto-expand)"
echo "  ✓ Конвертация --to-compact"
echo "  ✓ Импорт converted-файла"
echo -e "${NC}"
