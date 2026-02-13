# Исправление проблемы с Go Proxy

## Проблема

Go не может скачать зависимости из-за блокировки Google сервисов (403 Forbidden).

## Решение для Windows (PowerShell)

### Вариант 1: GOPROXY через goproxy.io (работает в РФ)

```powershell
# Установите переменные окружения
$env:GOPROXY = "https://goproxy.io,https://mirrors.aliyun.com/goproxy/,direct"
$env:GOSUMDB = "sum.golang.google.cn"

# Проверьте
go env | findstr GOPROXY

# Теперь выполните
cd G:\DEV\Go\TDTP\tdtp-framework\cmd\tdtp-xray
go mod download
go mod tidy
wails build
```

### Вариант 2: GOPROXY через goproxy.cn

```powershell
$env:GOPROXY = "https://goproxy.cn,direct"
$env:GOSUMDB = "off"

cd G:\DEV\Go\TDTP\tdtp-framework\cmd\tdtp-xray
go mod download
```

### Вариант 3: Глобальная настройка (рекомендуется)

```powershell
# Настроить для всей системы
go env -w GOPROXY=https://goproxy.io,https://goproxy.cn,direct
go env -w GOSUMDB=off

# Или через переменные окружения Windows
# 1. Win + R → sysdm.cpl
# 2. Advanced → Environment Variables
# 3. New:
#    GOPROXY = https://goproxy.io,direct
#    GOSUMDB = off
```

### Вариант 4: Через VPN (самый надёжный)

Если есть VPN, просто подключитесь и выполните:

```powershell
cd G:\DEV\Go\TDTP\tdtp-framework\cmd\tdtp-xray
go mod download
wails build
```

---

## Проверка

После настройки GOPROXY:

```powershell
# Проверьте настройки
go env GOPROXY
go env GOSUMDB

# Очистите кэш (если нужно)
go clean -modcache

# Попробуйте скачать заново
cd G:\DEV\Go\TDTP\tdtp-framework\cmd\tdtp-xray
go mod download
```

---

## Альтернатива: Скопировать готовый go.sum

Если совсем не получается скачать зависимости, можно скопировать `go.sum` из другого Wails проекта, который уже собран.

Но это НЕ рекомендуется - лучше настроить GOPROXY.

---

## Тестирование после настройки

```powershell
cd G:\DEV\Go\TDTP\tdtp-framework\cmd\tdtp-xray
wails build
```

Если сборка успешна - приложение будет в `.\build\bin\tdtp-xray.exe`
