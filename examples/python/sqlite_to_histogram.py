#!/usr/bin/env python3
"""
SQLite → TDTP XML → pandas → гистограмма балансов пользователей
================================================================

Сценарий:
  1. Читаем таблицу Users из SQLite (scripts/testdata/test.db)
  2. Конвертируем DataFrame → TDTP-словарь и пишем в users.tdtp.xml
  3. Читаем файл через TDTPClientJSON (полный round-trip)
  4. Преобразуем в pandas DataFrame с правильными типами
  5. Строим гистограмму распределения балансов по 10 группам

Запуск:
  cd examples/python
  TDTP_LIB_PATH=../../bindings/python/tdtp/libtdtp.so python sqlite_to_histogram.py

Зависимости:
  pip install pandas matplotlib
"""

import os
import sqlite3
import sys
from pathlib import Path

# ---------------------------------------------------------------------------
# Пути (работаем из любой директории)
# ---------------------------------------------------------------------------

SCRIPT_DIR = Path(__file__).parent
REPO_ROOT  = SCRIPT_DIR.parent.parent

DB_PATH      = REPO_ROOT / "scripts" / "testdata" / "test.db"
TDTP_OUT     = SCRIPT_DIR / "users.tdtp.xml"
HIST_OUT     = SCRIPT_DIR / "balance_histogram.png"

# ---------------------------------------------------------------------------
# Шаг 0 — проверки зависимостей
# ---------------------------------------------------------------------------

try:
    import pandas as pd
except ImportError:
    sys.exit("ERROR: установите pandas:  pip install pandas")

try:
    import matplotlib.pyplot as plt
    import matplotlib.ticker as mticker
except ImportError:
    sys.exit("ERROR: установите matplotlib:  pip install matplotlib")

# TDTP bindings берём из репо если явно не задан TDTP_LIB_PATH
_LIB_CANDIDATE = REPO_ROOT / "bindings" / "python" / "tdtp" / "libtdtp.so"
if "TDTP_LIB_PATH" not in os.environ and _LIB_CANDIDATE.exists():
    os.environ["TDTP_LIB_PATH"] = str(_LIB_CANDIDATE)

try:
    # Добавляем bindings/python в путь поиска модулей
    sys.path.insert(0, str(REPO_ROOT / "bindings" / "python"))
    from tdtp import TDTPClientJSON
    from tdtp.pandas_ext import pandas_to_data, data_to_pandas
except ImportError as exc:
    sys.exit(
        f"ERROR: не могу импортировать tdtp: {exc}\n"
        "Убедитесь что собрана libtdtp.so (make build-lib в bindings/python/)"
    )


# ===========================================================================
# ШАГ 1 — Чтение из SQLite
# ===========================================================================

print("=" * 60)
print("ШАГИ ПРИМЕРА: SQLite → TDTP → pandas → гистограмма")
print("=" * 60)

if not DB_PATH.exists():
    sys.exit(
        f"ERROR: База данных не найдена: {DB_PATH}\n"
        "Сначала создайте её:  python scripts/create_test_db.py"
    )

print(f"\n[1/5] Читаем Users из SQLite: {DB_PATH}")

conn   = sqlite3.connect(DB_PATH)
df_raw = pd.read_sql_query(
    "SELECT ID, Name, Email, Balance, IsActive, City, CreatedAt FROM Users",
    conn,
    dtype={
        "ID":       "Int64",
        "Balance":  "float64",
        "IsActive": "boolean",
    },
)
conn.close()

print(f"      Загружено {len(df_raw)} строк, колонки: {list(df_raw.columns)}")
print(df_raw[["ID", "Name", "Balance", "IsActive", "City"]].to_string(index=False))


# ===========================================================================
# ШАГ 2 — pandas DataFrame → TDTP XML
# ===========================================================================

print(f"\n[2/5] Конвертируем в TDTP и пишем в файл: {TDTP_OUT}")

client    = TDTPClientJSON()
tdtp_data = pandas_to_data(df_raw, table_name="Users")
client.J_write(tdtp_data, str(TDTP_OUT))

print(f"      Файл записан: {TDTP_OUT.stat().st_size} байт")
print(f"      Схема: {[f['Name'] + ':' + f['Type'] for f in tdtp_data['schema']['Fields']]}")


# ===========================================================================
# ШАГ 3 — Чтение обратно через TDTPClientJSON (round-trip)
# ===========================================================================

print(f"\n[3/5] Читаем файл через TDTPClientJSON (round-trip)")

raw_back = client.J_read(str(TDTP_OUT))
print(f"      Прочитано строк: {len(raw_back['data'])}")
print(f"      table_name: {raw_back['header']['table_name']}")


# ===========================================================================
# ШАГ 4 — TDTP dict → pandas DataFrame с правильными типами
# ===========================================================================

print(f"\n[4/5] Преобразуем TDTP-данные в pandas DataFrame")

df = data_to_pandas(raw_back)
df["Balance"] = pd.to_numeric(df["Balance"], errors="coerce")

print(f"      Строк: {len(df)}, колонок: {len(df.columns)}")
print(f"      Типы:\n{df.dtypes.to_string()}")
print(f"\n      Статистика Balance:")
stats = df["Balance"].describe()
for label, val in stats.items():
    print(f"        {label:<8}: {val:>10.2f}")


# ===========================================================================
# ШАГ 5 — Гистограмма распределения баланса по 10 группам
# ===========================================================================

print(f"\n[5/5] Строим гистограмму (10 групп) → {HIST_OUT}")

N_BINS = 10
bal    = df["Balance"].dropna()

fig, ax = plt.subplots(figsize=(10, 6))

counts, edges, patches = ax.hist(
    bal,
    bins=N_BINS,
    color="#4C72B0",
    edgecolor="white",
    linewidth=0.8,
)

# Подписываем каждый столбец
for count, patch in zip(counts, patches):
    if count > 0:
        ax.text(
            patch.get_x() + patch.get_width() / 2,
            patch.get_height() + 0.05,
            int(count),
            ha="center", va="bottom",
            fontsize=10, fontweight="bold",
        )

# Вертикальные линии — среднее и медиана
ax.axvline(bal.mean(),   color="#DD4949", linestyle="--", linewidth=1.4,
           label=f"Среднее  {bal.mean():.0f}")
ax.axvline(bal.median(), color="#2CA02C", linestyle=":",  linewidth=1.4,
           label=f"Медиана  {bal.median():.0f}")

# Подписи диапазонов на оси X
bin_labels = [f"{int(edges[i])}\n–\n{int(edges[i+1])}" for i in range(N_BINS)]
ax.set_xticks([(edges[i] + edges[i+1]) / 2 for i in range(N_BINS)])
ax.set_xticklabels(bin_labels, fontsize=8)

ax.yaxis.set_major_locator(mticker.MaxNLocator(integer=True))
ax.set_xlabel("Баланс (руб.)", fontsize=12)
ax.set_ylabel("Количество пользователей", fontsize=12)
ax.set_title(
    f"Распределение пользователей по балансу\n"
    f"(n={len(bal)}, {N_BINS} групп, источник: {DB_PATH.name} → TDTP → pandas)",
    fontsize=13,
)
ax.legend(fontsize=10)
ax.grid(axis="y", alpha=0.35)
fig.tight_layout()

fig.savefig(HIST_OUT, dpi=150)
print(f"      Гистограмма сохранена: {HIST_OUT}")

# Показываем в интерактивном режиме если есть дисплей
if os.environ.get("DISPLAY") or sys.platform == "darwin":
    plt.show()

print("\n" + "=" * 60)
print("Готово!")
print(f"  XML файл  : {TDTP_OUT}")
print(f"  Гистограмма: {HIST_OUT}")
print("=" * 60)
