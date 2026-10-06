# Oracle adapter (CLI v2 preview)

The Oracle adapter is registered only by `cmd/tdtpcli_v2`. It uses the pure-Go
`github.com/sijms/go-ora/v2` driver and the shared TDTP adapter contract.
The automated integration suite runs against Oracle XE 18c and 21c. Oracle 19c
belongs to the intended 18c–21c range, but has not yet had a live run.

Create a v2 config with `tdtpcli_v2 init-config oracle --output oracle.yaml`.
Set `database.host`, `port`, `database` (service name, e.g. `XEPDB1`), `user`
and `password`. A raw `database.dsn` overrides those fields. For example:

```yaml
database:
  type: oracle
  host: localhost
  port: 1521
  database: XEPDB1
  user: tdtp
  password: password
```

```sh
tdtpcli_v2 --config oracle.yaml list
tdtpcli_v2 --config oracle.yaml inspect-table ORDERS
tdtpcli_v2 --config oracle.yaml export ORDERS --where "ID > 10" --limit 5
tdtpcli_v2 --config oracle.yaml import orders.tdtp.xml --strategy replace
```

`type: oracle` is accepted for v2 pipeline sources. Its `query` is Oracle SQL;
the pipeline transform is still SQLite SQL. `NUMBER` fields are selected as
decimal text during export and pipeline reads to avoid `float64` rounding.
Oracle treats empty strings as `NULL`, so an empty string cannot round-trip
as a distinct value. Import supports `fail`, `copy`, `ignore` and `replace`;
the latter two require key fields. Oracle DDL commits implicitly, so a missing
target table is created before the transactional row import. A failed batch
rolls back its rows, but leaves that newly created empty table.

To run the live test suite, start Oracle XE 18c and 21c separately, create a
user with `CREATE TABLE` and `CREATE VIEW` privileges, and set:

```powershell
$env:TDTP_ORACLE18_DSN = 'oracle://user:password@127.0.0.1:1522/XEPDB1'
$env:TDTP_ORACLE21_DSN = 'oracle://user:password@127.0.0.1:1523/XEPDB1'
go test ./pkg/adapters/oracle -count=1 -v
```

Without those environment variables the unit tests still run and the live
subtests skip.
