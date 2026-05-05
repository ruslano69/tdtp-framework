import subprocess
import os
import sys

sys.stdout.reconfigure(encoding='utf-8', errors='replace')
sys.stderr.reconfigure(encoding='utf-8', errors='replace')

# Таблица: Анализ процентов
table_name = "Анализ_процентов"
cmd = [
    ".\\tdtpcli_x86.exe",
    "--config", "access_delo19.yaml",
    "--export", table_name,
    "--output", "DELO19/test_fix_encoding.tdtp.xml",
    "--limit", "5",
    "--compress=false"
]

print(f"Running: {' '.join(cmd)}")
result = subprocess.run(cmd, capture_output=True, text=True, encoding='utf-8')

print("STDOUT:", result.stdout)
print("STDERR:", result.stderr)
