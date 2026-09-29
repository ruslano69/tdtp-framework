# Практический кейс: проверка CSV с больничными до импорта

Этот пример показывает цепочку **CSV → ETL/SQLite SQL → один TDTP**. SQL
проверяет каждую запись, а результат сохраняет как правильные, так и
ошибочные строки с колонками `validation_status` и `validation_error`.
Импорт в рабочую базу пример не выполняет.

Формат CSV здесь **иллюстративный**: реальные поля и порядок нужно сверить с
фактической выгрузкой. Этот кейс относится к CSV, полученному из реестра
или из другой системы; он не описывает официальный формат выгрузки ПФУ.

Файлы примера:

| Файл | Назначение |
|------|------------|
| [`demo-input.csv`](demo-input.csv) | Два обезличенных больничных: корректный и с датой `2026-02-31` |
| [`pipeline.yaml`](pipeline.yaml) | CSV-источник, SQL-проверки, вывод TDTP |
| [`output/demo-result.tdtp.xml`](output/demo-result.tdtp.xml) | Готовый результат для даты проверки `2026-09-29` и лимита 30 дней |

## Повторить пример

Из корня репозитория в PowerShell:

```powershell
go build -trimpath -ldflags '-s -w' -o tdtpcli_v2.exe ./cmd/tdtpcli_v2

.\tdtpcli_v2.exe pipeline examples\pfu-csv\pipeline.yaml `
  '@csv_path=examples/pfu-csv/demo-input.csv' `
  '@output_path=examples/pfu-csv/output/my-run.tdtp.xml' `
  '@reference_date=2026-09-29' `
  '@max_days=30'

.\tdtpcli_v2.exe test examples\pfu-csv\output\my-run.tdtp.xml
.\tdtpcli_v2.exe to-csv examples\pfu-csv\output\my-run.tdtp.xml `
  --fields NUM_LN,validation_status,validation_error `
  --output examples\pfu-csv\output\review.csv
```

Ожидаемый результат просмотра:

```csv
NUM_LN,validation_status,validation_error
123456-1234567890-1,OK,
654321-0987654321-2,ERROR,DATE_START is not a valid YYYY-MM-DD date
```

Для рабочего файла передайте свой путь в `@csv_path`, текущую дату по Киеву в
`@reference_date` и согласованный положительный максимум дней в `@max_days`.
Число `30` здесь только значение **для демонстрации**, не норматив ПФУ.
`@output_path` задаёт место нового файла. Оба CLI (`tdtpcli.exe --pipeline`
и `tdtpcli_v2.exe pipeline`) используют один и тот же ETL-модуль.

## Что проверяет SQL

Входной файл содержит заголовок и восемь колонок, разделённых `;`:

```text
NUM_LN;NUM_CASE;VERSION;RN_OKP;FIO;DATE_START;DATE_END;STATUS
```

Загрузчик сохраняет ячейки как `TEXT`: даже неверная дата остаётся видимой.
SQL проверяет обязательные поля, положительную целую версию, календарную
корректность дат `YYYY-MM-DD`, порядок дат, длительность включительно и
`DATE_START` в окне ±1 календарный месяц от переданной даты. Концы месяца
ограничиваются последним днём соответствующего месяца. В `validation_error`
попадает первая найденная причина; у правильной строки статус `OK` и пустая
ошибка. Исходные значения и порядок строк сохраняются в одном TDTP.

Структурно повреждённый CSV, лишняя или недостающая колонка, несовпадающий
заголовок или неверная кодировка останавливают ETL до создания результата.
Для Windows-1251 измените `csv.encoding` в YAML. Для файла без заголовка
укажите `csv.header: false`; порядок `csv.columns` всё равно обязателен.

Для следующего этапа берите только строки со статусом `OK`. Проверяйте
целостность TDTP командой `test` перед импортом. В текущем выходном пакете
исходные даты и версия остаются `TEXT`, чтобы ошибочные исходные значения
можно было увидеть и исправить.
