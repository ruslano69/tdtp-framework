import os
import re
import subprocess

def translit(text):
    if not text: return "empty"
    mapping = {
        'а': 'a', 'б': 'b', 'в': 'v', 'г': 'g', 'д': 'd', 'е': 'e', 'ё': 'e', 'ж': 'zh', 'з': 'z',
        'и': 'i', 'й': 'y', 'к': 'k', 'л': 'l', 'м': 'm', 'н': 'n', 'о': 'o', 'п': 'p', 'р': 'r',
        'с': 's', 'т': 't', 'у': 'u', 'ф': 'f', 'х': 'kh', 'ц': 'ts', 'ч': 'ch', 'ш': 'sh', 'щ': 'sch',
        'ъ': '', 'ы': 'y', 'ь': '', 'э': 'e', 'ю': 'yu', 'я': 'ya',
        'А': 'A', 'Б': 'B', 'В': 'V', 'Г': 'G', 'Д': 'D', 'Е': 'E', 'Ё': 'E', 'Ж': 'Zh', 'З': 'Z',
        'И': 'I', 'Й': 'Y', 'К': 'K', 'Л': 'L', 'М': 'M', 'Н': 'N', 'О': 'O', 'П': 'P', 'Р': 'R',
        'С': 'S', 'Т': 'T', 'У': 'U', 'Ф': 'F', 'Х': 'Kh', 'Ц': 'Ts', 'Ч': 'Ch', 'Ш': 'Sh', 'Щ': 'Sch',
        'Ъ': '', 'Ы': 'Y', 'Ь': '', 'Э': 'E', 'Ю': 'Yu', 'Я': 'Ya',
        'і': 'i', 'І': 'I', 'ї': 'yi', 'Ї': 'Yi', 'є': 'ye', 'Є': 'Ye', 'ґ': 'g', 'Ґ': 'G'
    }
    res = "".join(mapping.get(char, char) for char in text)
    res = res.replace("?", "_").replace("/", "_").replace("%", "_pct").replace("$", "_usd").replace(" ", "_").replace("№", "N")
    res = re.sub(r'[^\w]', '_', res)
    res = re.sub(r'_+', '_', res).strip('_')
    if res and res[0].isdigit(): res = "t_" + res
    return res

src_dir = "DELO19"
fixed_dir = "DELO19_fixed"
cli = ".\\tdtpcli_x86.exe"
config = "configs/config.sqlite-import.yaml"

if not os.path.exists(fixed_dir):
    os.makedirs(fixed_dir)

all_files = [f for f in os.listdir(src_dir) if f.endswith(".xml")]
print(f"1. Fixing and copying {len(all_files)} files...")

for filename in all_files:
    path = os.path.join(src_dir, filename)
    with open(path, 'r', encoding='utf-8') as f:
        content = f.read()
    
    # Extract original table name
    m_table = re.search(r'<TableName>(.*?)</TableName>', content)
    if not m_table: continue
    old_table = m_table.group(1)
    new_table = translit(old_table)
    
    # Replace TableName
    content = content.replace(f"<TableName>{old_table}</TableName>", f"<TableName>{new_table}</TableName>")
    
    # Replace all field names
    fields = re.findall(r'<Field name="(.*?)"', content)
    for old_field in set(fields):
        new_field = translit(old_field)
        content = content.replace(f'name="{old_field}"', f'name="{new_field}"')
        
    with open(os.path.join(fixed_dir, filename), 'w', encoding='utf-8') as f:
        f.write(content)

# Run import for Part 1 or single files
import_targets = [f for f in all_files if "_part_" not in f or "_part_1_of_" in f]
print(f"\n2. Importing {len(import_targets)} tables...")

success = 0
failed = 0

for filename in import_targets:
    fixed_path = os.path.join(fixed_dir, filename)
    # Get table name from file content (could be different from filename)
    with open(fixed_path, 'r', encoding='utf-8') as f:
        content = f.read(2048) # Read only header
    m_table = re.search(r'<TableName>(.*?)</TableName>', content)
    table_name = m_table.group(1) if m_table else filename
    
    print(f"Importing {table_name} ... ", end="", flush=True)
    cmd = [cli, "--import", fixed_path, "--config", config, "--table", table_name, "--strategy", "replace"]
    result = subprocess.run(cmd, capture_output=True, text=True, encoding='utf-8')
    
    if result.returncode == 0:
        print("OK")
        success += 1
    else:
        print("FAIL")
        failed += 1

print(f"\nFinal Result: {success} success, {failed} failed.")
