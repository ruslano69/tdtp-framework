# TDTP X-Ray — Сборка

## Требования

- **Go 1.25+**
- **Wails CLI v2.11+**
- **Windows 10/11** (для нативной сборки)
- **WebView2 Runtime** — обычно уже установлен в Windows 11; для Windows 10: [скачать](https://developer.microsoft.com/microsoft-edge/webview2/)

### Установка Wails

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

Убедитесь что `%GOPATH%\bin` в `PATH`:

```powershell
$env:Path += ";$(go env GOPATH)\bin"
wails version
```

---

## Сборка EXE

```powershell
cd tdtp-framework\cmd\tdtp-xray
wails build
```

Результат: `build\bin\tdtp-xray.exe`

> **Важно:** Wails читает фронтенд напрямую из `frontend/src/` (см. `wails.json` → `"assetdir"`).
> Копировать файлы в `frontend/dist/` **не нужно**.

### Без консольного окна (рекомендуется для релиза)

```powershell
wails build -windowsconsole
```

Или без подсказок (тихая сборка):

```powershell
wails build -nocolour
```

---

## Режим разработки (hot-reload)

```powershell
cd tdtp-framework\cmd\tdtp-xray
wails dev
```

- Go-бэкенд перекомпилируется при изменении `.go` файлов
- Фронтенд обновляется при изменении файлов в `frontend/src/`
- DevTools доступны по `F12`

---

## Кросс-компиляция с Linux → Windows

Для сборки `.exe` с Linux-машины (например, CI):

```bash
# Установить MinGW-w64
sudo apt-get install gcc-mingw-w64-x86-64

# Собрать
cd tdtp-framework/cmd/tdtp-xray
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
  wails build -platform windows/amd64
```

---

## Структура проекта

```
cmd/tdtp-xray/
├── main.go              # Точка входа Wails
├── app.go               # Go API (бэкенд, ~2500 строк)
├── wails.json           # Конфигурация Wails (assetdir → frontend/src)
├── go.mod               # Go 1.25
├── services/            # Сервисы бизнес-логики
│   ├── connection_service.go
│   ├── metadata_service.go
│   ├── preview_service.go
│   ├── source_service.go
│   └── tdtp_service.go
├── frontend/
│   └── src/             # ← Wails берёт файлы отсюда напрямую
│       ├── index.html
│       ├── styles/
│       │   ├── main.css
│       │   └── wizard.css
│       └── scripts/
│           └── wizard.js  # Вся логика UI (~3800 строк)
└── build/               # Результат wails build (gitignored)
    └── bin/
        └── tdtp-xray.exe
```

---

## Рабочий процесс разработки

```
1. Правите frontend/src/ или *.go
2. wails build
3. .\build\bin\tdtp-xray.exe
```

Или `wails dev` для автоперезагрузки.

---

## Устранение проблем

### `wails: command not found`
```powershell
$env:Path += ";$(go env GOPATH)\bin"
```

### `go.mod requires go >= 1.25 (running go 1.XX)`
Обновите Go: https://go.dev/dl/

### `Cannot find wails.json`
Убедитесь что вы в правильной директории:
```powershell
cd tdtp-framework\cmd\tdtp-xray
```

### Приложение запускается но экран пустой
Нет WebView2. Установите: https://developer.microsoft.com/microsoft-edge/webview2/

### `Failed to find Vite server URL`
Игнорируйте — проект использует Vanilla JS без Vite/npm.
