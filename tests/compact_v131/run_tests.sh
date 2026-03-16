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
grep -q 'compact="true"' $DIR/dept_emp_compact.xml  && ok 'Data compact="true"'    || fail "compact не установлен"
grep -q 'version="1.3.1"' $DIR/dept_emp_compact.xml && ok 'version="1.3.1"'        || fail "версия не обновлена"

# 2a. Schema: _prefix stripped, все 3 fixed поля → fixed="true", остальные НЕ fixed
hdr "2a. Schema: _ stripped + ровно 3 fixed поля, 4 variable"
grep -q 'name="_dept_id"'   $DIR/dept_emp_compact.xml && fail "_dept_id не stripped" || ok "_dept_id → dept_id (stripped)"
grep -q 'name="_dept_name"' $DIR/dept_emp_compact.xml && fail "_dept_name не stripped" || ok "_dept_name → dept_name (stripped)"
grep -q 'name="_location"'  $DIR/dept_emp_compact.xml && fail "_location не stripped" || ok "_location → location (stripped)"

# Каждое из трёх fixed полей должно иметь fixed="true" рядом с именем
python3 - <<'PYEOF'
import xml.etree.ElementTree as ET, sys
tree = ET.parse("/tmp/test_compact/dept_emp_compact.xml")
schema = tree.getroot().find("Schema")
fields = schema.findall("Field")

fixed   = [f.get("name") for f in fields if f.get("fixed") == "true"]
variable = [f.get("name") for f in fields if f.get("fixed") != "true"]

expected_fixed    = {"dept_id", "dept_name", "location"}
expected_variable = {"emp_id", "full_name", "salary", "hire_date"}

errors = []
if set(fixed) != expected_fixed:
    errors.append(f"fixed fields: ожидалось {expected_fixed}, получено {set(fixed)}")
if set(variable) != expected_variable:
    errors.append(f"variable fields: ожидалось {expected_variable}, получено {set(variable)}")

if errors:
    for e in errors: print(f"FAIL: {e}")
    sys.exit(1)
else:
    print(f"OK: fixed={fixed}")
    print(f"OK: variable={variable}")
PYEOF
ok "3 fixed, 4 variable — всё верно"

# 2b. Compact-строки: первая строка каждой группы имеет значения, остальные — |||
hdr "2b. Compact Data: групповая структура строк"
info "Все строки Data:"
grep '<R>' $DIR/dept_emp_compact.xml | cat -n

# Группа dept 10 (5 сотрудников): 1 header + 4 carry строки
GROUP10_HEADER=$(grep '<R>10|' $DIR/dept_emp_compact.xml | wc -l)
info "Строк с dept_id=10 (header группы): $GROUP10_HEADER (ожидается 1)"
[ "$GROUP10_HEADER" -eq 1 ] && ok "dept 10 header row" || fail "Ожидалось 1, получено $GROUP10_HEADER"

GROUP20_HEADER=$(grep '<R>20|' $DIR/dept_emp_compact.xml | wc -l)
info "Строк с dept_id=20 (header группы): $GROUP20_HEADER (ожидается 1)"
[ "$GROUP20_HEADER" -eq 1 ] && ok "dept 20 header row" || fail "Ожидалось 1, получено $GROUP20_HEADER"

GROUP30_HEADER=$(grep '<R>30|' $DIR/dept_emp_compact.xml | wc -l)
info "Строк с dept_id=30 (header группы): $GROUP30_HEADER (ожидается 1)"
[ "$GROUP30_HEADER" -eq 1 ] && ok "dept 30 header row" || fail "Ожидалось 1, получено $GROUP30_HEADER"

# 12 carry-строк (по 4 на каждый отдел из 5 сотрудников)
CARRY=$(grep '<R>|||' $DIR/dept_emp_compact.xml | wc -l)
info "Carry-forward строк (|||...): $CARRY (ожидается 12)"
[ "$CARRY" -eq 12 ] && ok "12 carry-forward строк" || fail "Ожидалось 12, получено $CARRY"

# Проверяем точные header-строки каждой группы через grep
grep -q '<R>10|Sales|Moscow|101|Ivan Petrov'              $DIR/dept_emp_compact.xml && ok "dept 10 header: 10|Sales|Moscow|101|Ivan Petrov"       || fail "dept 10 header не найден"
grep -q '<R>20|Engineering|Saint Petersburg|201|Alice Volkov' $DIR/dept_emp_compact.xml && ok "dept 20 header: 20|Engineering|Saint Petersburg|201" || fail "dept 20 header не найден"
grep -q '<R>30|HR|Kazan|301|George Orlov'                 $DIR/dept_emp_compact.xml && ok "dept 30 header: 30|HR|Kazan|301|George Orlov"         || fail "dept 30 header не найден"

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

# Проверка данных: все 3 группы × все 3 fixed поля + граничные сотрудники
hdr "5a. Проверка carry-forward по всем 3 группам (dept 10 / 20 / 30)"
python3 - <<PYEOF
import sqlite3, sys
con = sqlite3.connect("$DB")
cur = con.cursor()

# Ожидаемые значения: (emp_id, dept_id, dept_name, location, full_name)
checks = [
    # dept 10 — Sales / Moscow
    (101, 10, "Sales",       "Moscow",           "Ivan Petrov"),     # первый, header row
    (103, 10, "Sales",       "Moscow",           "Boris Kozlov"),     # середина группы
    (105, 10, "Sales",       "Moscow",           "Dmitry Smirnov"),   # последний группы
    # dept 20 — Engineering / Saint Petersburg (граница группы!)
    (201, 20, "Engineering", "Saint Petersburg", "Alice Volkov"),     # первый, header row
    (203, 20, "Engineering", "Saint Petersburg", "Diana Popova"),     # середина
    (205, 20, "Engineering", "Saint Petersburg", "Fiona Kuznetsova"), # последний
    # dept 30 — HR / Kazan (граница группы!)
    (301, 30, "HR",          "Kazan",            "George Orlov"),     # первый, header row
    (303, 30, "HR",          "Kazan",            "Igor Fedorov"),     # середина
    (305, 30, "HR",          "Kazan",            "Kirill Sokolov"),   # последний
]

errors = []
for emp_id, exp_dept_id, exp_dept_name, exp_location, exp_name in checks:
    cur.execute(
        "SELECT dept_id, dept_name, location, full_name FROM dept_emp_imported WHERE emp_id=?",
        (emp_id,)
    )
    row = cur.fetchone()
    if row is None:
        errors.append(f"emp_id={emp_id}: строка не найдена")
        continue
    dept_id, dept_name, location, full_name = row
    ok = (dept_id == exp_dept_id and dept_name == exp_dept_name
          and location == exp_location and full_name == exp_name)
    marker = "OK" if ok else "FAIL"
    print(f"  {marker} emp={emp_id}: dept_id={dept_id} dept_name={dept_name!r} location={location!r} name={full_name!r}")
    if not ok:
        errors.append(
            f"emp_id={emp_id}: ожидалось ({exp_dept_id},{exp_dept_name!r},{exp_location!r},{exp_name!r}), "
            f"получено ({dept_id},{dept_name!r},{location!r},{full_name!r})"
        )

con.close()
if errors:
    for e in errors: print(f"FAIL: {e}")
    sys.exit(1)
PYEOF
ok "Все 9 проверок по 3 группам прошли (carry-forward корректен)"

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
echo "  ✓ Schema: 3 fixed (dept_id/dept_name/location), 4 variable — _ stripped"
echo "  ✓ Compact-строки: 3 header rows + 12 carry-forward (|||)"
echo "  ✓ Экспорт с явным --fixed-fields"
echo "  ✓ Импорт compact (auto-expand): 9 сотрудников × 3 группы — всё верно"
echo "  ✓ Конвертация --to-compact"
echo "  ✓ Импорт converted-файла"
echo -e "${NC}"
