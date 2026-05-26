# read-tdtp — Чтение TDTP файла

Минимальный пример чтения и вывода содержимого TDTP файла.  
Поддерживает сжатые пакеты (`compression="zstd"` / `"kanzi"`).

## Запуск

```bash
# Читать конкретный файл
go run main.go path/to/file.tdtp.xml

# По умолчанию ищет tdtp.xml в текущей директории
go run main.go
```

## Вывод

```
Table: users | rows: 3

  id:             1
  name:           Alice
  email:          alice@example.com

  id:             2
  name:           Bob
  ...
```

## Ключевые API

```go
parser := packet.NewParser()
pkt, _ := parser.ParseWithDecompression(f, func(_ context.Context, compressed, algo string) ([]string, error) {
    return processors.DecompressDataForTdtpWithAlgo(compressed, algo)
})

for _, row := range pkt.GetRows() { ... }
```

## См. также

- [`examples/basic`](../basic) — создание и парсинг пакетов через Go API
- `tdtpcli --inspect file.tdtp.xml` — metadata без Go кода
