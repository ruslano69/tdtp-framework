# MS Access → TDTP XML Export

Python-скрипт для полного экспорта всех таблиц из базы Microsoft Access (`.mdb` / `.accdb`) в формат TDTP XML через `tdtpcli_x86.exe`.

## Требования

| Компонент | Версия | Примечание |
|-----------|--------|------------|
| Windows | XP+ | Только Windows (Jet ODBC — Win32 компонент) |
| Microsoft Access Driver | любая | Входит в Windows по умолчанию (`odbcad32.exe` → 32-bit) |
| Python | 3.x | Стандартная библиотека, без зависимостей |
| `tdtpcli_x86.exe` | 1.9+ | Собрать с `GOARCH=386` (см. ниже) |

## Сборка tdtpcli_x86.exe

```powershell
# из корня репозитория
$env:GOPROXY="https://goproxy.io"; $env:GONOSUMDB="*"; $env:GOARCH="386"
go build -tags nokafka -o tdtpcli_x86.exe ./cmd/tdtpcli/
$env:GOARCH=""
```

> Почему `GOARCH=386`? Microsoft Jet 4.0 ODBC — 32-bit компонент.  
> 64-bit процесс не может подключиться к нему напрямую.

## Установка

Положите в одну папку:

```
export_access_db.py   ← этот скрипт
tdtpcli_x86.exe       ← 32-bit бинарник
database.mdb          ← ваша база данных
system.mda            ← файл рабочей группы (если есть)
```

Откройте скрипт и при необходимости скорректируйте переменные в начале:

```python
MDB_PATH = os.path.join(SCRIPT_DIR, "database.mdb")  # путь к .mdb
MDA_PATH = os.path.join(SCRIPT_DIR, "system.mda")     # путь к .mda (или оставьте — пропустится если файла нет)
UID      = "Admin"                                      # логин
PWD      = ""                                           # пароль
CHARSET  = "windows-1251"                               # кодировка данных; "" если UTF-8
```

## Запуск

```bash
python export_access_db.py
```

Скрипт:
1. Проверяет наличие обязательных файлов
2. Получает список таблиц через `tdtpcli_x86.exe --list`
3. Экспортирует каждую таблицу в `./tdtp_export/<имя>.tdtp.xml`

Пример вывода:
```
Database : C:\work\database.mdb
Output   : C:\work\tdtp_export
Tables   : 50

  OK  Товары                                     1,667,873 bytes  0.4s
  OK  Цена товара                                  706,280 bytes  0.3s
  OK  Организации                                  263,619 bytes  0.2s
  ...

============================================================
Done: 50 exported, 0 failed
Output: C:\work\tdtp_export
```

## Дальнейшая работа с файлами

```bash
# Просмотр структуры (поля, типы, количество строк)
tdtpcli_x86.exe --inspect Товары.tdtp.xml

# Просмотр в браузере
tdtpcli_x86.exe --to-html Товары.tdtp.xml --open

# Конвертация в Excel
tdtpcli_x86.exe --to-xlsx Товары.tdtp.xml --output Товары.xlsx

# Импорт в PostgreSQL / MSSQL / SQLite
tdtpcli_x86.exe --config target.yaml --import Товары.tdtp.xml --strategy replace
```

## Кодировка

Старые базы Access (Jet 2.x / 3.x) в русской Windows хранят строки в **Windows-1251**.  
`alexbrainman/odbc` использует Unicode ODBC API для имён колонок (всегда UTF-8),  
но данные из Jet 2.x могут приходить как ANSI-байты.

Параметр `charset: windows-1251` в конфиге включает конвертацию через `charmap.Windows1251`  
перед записью в XML. Без него в файле окажутся сырые CP1251 байты и XML будет невалидным.

## Ссылки

- [Документация адаптера Access](../../../pkg/adapters/access/README.md)
- [TDTP Framework](https://github.com/ruslano69/tdtp-framework-main)
