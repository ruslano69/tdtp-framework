import sys, sqlite3
sys.stdout.reconfigure(encoding="utf-8")
con = sqlite3.connect("out/edm_sqlite.db")
cur = con.cursor()
cur.execute("SELECT sql FROM sqlite_master WHERE type='table' AND name='employees'")
print(cur.fetchone()[0])
print()
cur.execute("SELECT ext_id, display_name, contract_type, hired_at, birth_date, sex, work_years FROM employees")
for row in cur.fetchall():
    print(row)
