# Proposal: column constraints in the TDTP Schema

**Status: DRAFT — not part of the format.** Nothing here is written into
packets, read by the parser, or accepted by `docs/tdtp.xsd`. The prototype
that measured it is read-only: `pkg/adapters/mssql/constraints.go`,
`checkparse.go`, and `cmd/tdtp-constraints-probe`.

Source database for the first cut: **Microsoft SQL Server**. Every "stored
as" below was measured on SQL Server 2019 (15.0, CU32), not taken from
documentation.

## 1. What a packet loses today

A TDTP Schema carries each column's type, length, precision/scale, key
membership, subtype and read-only flag. Measured against a table with the
usual constraints, it drops:

| Property | Where SQL Server keeps it | In the packet today |
|---|---|---|
| NOT NULL | `sys.columns.is_nullable` | **read and discarded** — `mssql/export.go` selects `IS_NULLABLE`, `packet.Field` has nowhere to put it. On import only key columns get `NOT NULL`. |
| Primary key column **order** | `sys.index_columns.key_ordinal` | `key="true"` per field, so order = column order. `PK (TabNo, Branch)` comes back as `(Branch, TabNo)`: same uniqueness, different clustered index. |
| UNIQUE keys | `sys.indexes` (`is_unique_constraint`, `is_unique`) | absent |
| Ranges, lists, patterns | `sys.check_constraints` | absent |
| Defaults | `sys.default_constraints` | absent |
| Column description | `sys.extended_properties` (`MS_Description`) | absent |

A composite key is **not** missing: several fields with `key="true"` already
form one, and the adapters create `PRIMARY KEY (a, b)` from it. What is
missing is its order.

## 2. What SQL Server actually stores

Ranges and patterns are **not column properties** in SQL Server. They exist
only as CHECK constraints — arbitrary T-SQL expressions — and the server
rewrites them before storing:

| Written | Stored in `sys.check_constraints.definition` |
|---|---|
| `Age BETWEEN 18 AND 70` | `([Age]>=(18) AND [Age]<=(70))` |
| `Status IN ('active','leave','fired')` | `([Status]='fired' OR [Status]='leave' OR [Status]='active')` |
| `Status='x1' OR Status='x2'` | `([Status]='x1' OR [Status]='x2')` — **not** reversed |
| `Zip LIKE '[0-9][0-9][0-9][0-9][0-9]'` | `([Zip] like '[0-9][0-9][0-9][0-9][0-9]')` |
| `LEN(Inn) = 10` | `(len([Inn])=(10))` |
| `DEFAULT (0)` / `DEFAULT ('active')` / `DEFAULT (getdate())` | `((0))` / `('active')` / `(getdate())` |

Two consequences:

- **One canonical form to parse**, not all of T-SQL. BETWEEN and IN never
  reach the catalog. The prototype's grammar is a few dozen lines: bracketed
  identifiers, parenthesized literals, AND/OR chains, LIKE, IS [NOT] NULL.
- **Enumeration order is not recoverable.** IN is stored reversed, a
  hand-written OR is stored as written, and the stored text cannot tell them
  apart. An enumeration in the format is therefore a **set**.

## 3. Phase 1 — nullability and keys (the candidate to specify first)

Cheap, exact, available from every database the framework supports, and it
closes a loss that happens on every transfer today.

```xml
<Schema>
  <Field name="Branch"   type="INTEGER" key="true"/>
  <Field name="TabNo"    type="INTEGER" key="true"/>
  <Field name="FullName" type="TEXT" length="200" nullable="false"/>
  <Field name="Email"    type="TEXT" length="120"/>
  <Constraints>
    <PrimaryKey name="PK_Employees">
      <Column name="TabNo"/>
      <Column name="Branch"/>
    </PrimaryKey>
    <Unique name="UQ_Emp_Email"><Column name="Email"/></Unique>
  </Constraints>
</Schema>
```

**Rules.**

1. `nullable` absent means nullable. That is exactly what every packet
   written so far already means to an importer, so old packets need no
   reinterpretation. Producers write only `nullable="false"`. Key columns are
   NOT NULL by definition and need not say so.
2. `key="true"` stays — it is how every existing reader finds the key.
   `<PrimaryKey>` adds only the order and is written when the key has two or
   more columns. When present it must name exactly the `key="true"` fields
   (a semantics-layer check: XSD cannot compare two sets).
3. `<Column name>` must name a `<Field>`. XSD 1.0 **can** express this:
   `xs:key` on `Field/@name` plus `xs:keyref` from `Constraints/*/Column/@name`
   — verified on the vendored engine, which answers
   `keyref does not resolve` for a dangling name. The same `xs:key` also
   rejects duplicate field names, a check the semantics layer does today.
4. `<Unique>` covers UNIQUE constraints and unique indexes alike (same
   guarantee). A **filtered** unique index (`WHERE ...`) is unique on a subset
   of rows, not a key, and is not carried.

**XSD sketch** (added to `SchemaType` after `Dictionary`; attribute on
`FieldType`):

```xml
<xs:attribute name="nullable" type="xs:boolean" use="optional"/>

<xs:element name="Constraints" minOccurs="0">
  <xs:complexType><xs:sequence>
    <xs:element name="PrimaryKey" type="KeyType" minOccurs="0"/>
    <xs:element name="Unique"     type="KeyType" minOccurs="0" maxOccurs="unbounded"/>
    <xs:element name="Check"      type="CheckType" minOccurs="0" maxOccurs="unbounded"/>
  </xs:sequence></xs:complexType>
</xs:element>

<!-- on the Schema element declaration -->
<xs:key name="fieldName"><xs:selector xpath="Field"/><xs:field xpath="@name"/></xs:key>
<xs:keyref name="constraintColumn" refer="fieldName">
  <xs:selector xpath="Constraints/*/Column"/><xs:field xpath="@name"/>
</xs:keyref>
```

**Open for phase 1:** UNIQUE and NULL disagree between engines. SQL Server
allows one NULL in a unique column; PostgreSQL and the SQL standard allow
many. A packet moving from SQL Server to PostgreSQL loses nothing, the other
way round the import can fail. Either record the source semantics
(`nulls="single|distinct"`) or document it as a known cross-engine
difference, like `length` units under `--strict-schema`.

## 4. Phase 2 — ranges, lists, patterns

Facets on `<Field>`, named after the XSD facets they mirror, values in the
**same text form as row values of that field** (so the validator compares them
with the converter it already uses):

```xml
<Field name="Age"    type="INTEGER" minInclusive="18" maxInclusive="70"/>
<Field name="Rate"   type="DECIMAL" precision="4" scale="2" minExclusive="0" maxInclusive="1.5"/>
<Field name="Status" type="TEXT" length="10" nullable="false">
  <Enum><Value>fired</Value><Value>leave</Value><Value>active</Value></Enum>
</Field>
<Field name="Zip"    type="TEXT" length="5" pattern="[0-9][0-9][0-9][0-9][0-9]"/>
<Constraints>
  <Check name="CK_Emp_Inn" dialect="mssql">(len([Inn])=(10))</Check>
</Constraints>
```

**What maps, from the stored form:**

| Stored CHECK | Facet |
|---|---|
| `[c]>=(n)`, `>`, `<=`, `<`, AND-chains of them | `min/maxInclusive`, `min/maxExclusive` |
| OR-chain of `[c]=literal` | `<Enum>` (a set, see §2) |
| `[c] like '…'`, OR-chain of LIKEs | `pattern` (alternatives joined with `\|`) |
| `[c] IS NULL OR <any of the above>` | the same facet (a CHECK already passes on NULL) |
| anything else — functions, two columns, `<>`, `NOT`, mixed OR | `<Check dialect="mssql">` raw text |

Range implied by the type is not repeated: `tinyint` is 0..255 and
`decimal(4,2)` is ±99.99 from `subtype`/`precision`/`scale`, which the packet
already carries.

**The pattern language is XSD regular expressions**, implicitly anchored,
because the schema is the contract other SDKs validate against with their own
XSD engines. The prototype converts LIKE to it: `%` → `.*`, `_` → `.`,
`[..]`/`[^..]` kept, `ESCAPE` honoured, every other metacharacter escaped in
a form that is valid in both XSD and Go (`$`, which XSD cannot escape, goes
out as `[$]`). A test runs each converted pattern through both the vendored
XSD engine and Go's `regexp` on matching and non-matching values.

**Raw `<Check>` is information, not a rule.** It is carried so a human or a
same-engine import can use it; no other engine is expected to evaluate
T-SQL. The `dialect` attribute says whose expression it is.

**Where the facets are enforced.** Not by the XSD: row values sit inside
pipe-joined `<R>` text the schema cannot see. By the validator's semantics
layer (each value against its field's facets), and by the target database
once an import recreates the constraints.

**Recreating constraints on import is opt-in** — the same switch as
`--strict-schema`. Commit `7887de4` removed `VARCHAR(n)` recreation because
it truncated data on import; turning facets into `CHECK`s by default would
fail imports for anyone whose data already strays from them. Off by default,
loud when on.

**Measured on the probe table** (§7): 10 CHECKs → 8 facets on 8 columns,
2 raw (`len()` and a two-column date comparison), each with its reason. The
number that matters is the same ratio on a real schema — see §8.

**Open for phase 2:**

- **Case sensitivity.** LIKE follows the column collation — `_CI_` almost
  everywhere — an XSD pattern is always case-sensitive. A source that accepted
  `a1` for `[A-Z][0-9]%` produces a pattern that rejects it. Options: a
  `patternCase="insensitive"` attribute (validator folds case; XSD engines
  ignore it), or expanding letters to `[aA]` classes (exact, ugly). The
  prototype flags it only when the pattern contains letters — `[0-9]…` and
  `+7%` mean the same under any collation.
- **Untrusted CHECKs** (`WITH NOCHECK`, `is_not_trusted = 1`): existing rows
  were never verified. Emit the facet and mark it, or drop it? The prototype
  flags it. **Disabled** CHECKs (`is_disabled = 1`) do not hold at all and are
  not emitted.
- **Two CHECKs on one facet** (`BETWEEN 18 AND 70` and `>= 21`): the effective
  minimum is 21, but choosing is a guess. The prototype keeps the first in
  constraint-name order as a facet and the second whole as raw text.
- **CHAR padding.** `char(5)` values are blank-padded; LIKE sees the padding.
  Worth a test before patterns on CHAR columns are trusted.

## 5. Phase 3 — defaults, descriptions, foreign keys

- **Defaults:** literals as `default="…"` (prototype: `((0))` → `0`,
  `(N'x')` → `x`); expressions (`getdate()`, `newid()`) are dialect text and
  either travel as `<Default dialect="mssql">` or not at all.
- **Descriptions:** `MS_Description` → `description="…"`. Cheap; useful to
  XLSX headers and generated docs.
- **Foreign keys:** descriptive only — a packet holds one table, so nothing
  can check them — but they give the load order for multi-table sync, which
  the travel-agency coordinator encodes by hand today. `--inspect-table`
  already reads them from `sys.foreign_keys`.

## 6. How it is switched on (agreed)

| Side | What it does | Switch |
|---|---|---|
| `export`, `export-broker` | write constraints into `<Schema>` | phase 1: always; phase 2: `--constraints`, config `export.constraints: true` |
| `validate` | check row values against the facets a packet carries | none — facets in a packet are there to be checked |
| `import` | recreate constraints in the target database | the existing `--strict-schema` |

**Phase 1 needs no switch.** It repairs a loss — `IS_NULLABLE` is read and
dropped on every export today — and an absent `nullable` reads as nullable,
exactly as now. What it costs is a protocol version, not a behaviour change
a flag would have to guard.

**Phase 2 is opt-in**, at least until §4's open questions close: facets make
the validator stricter (a packet that was VALID can become INVALID), and
some are fuzzier than their source (`_CI_` patterns, untrusted CHECKs).

- Flag over config the v2 way: a **given** `--constraints` wins
  (`--constraints=false` switches off `export.constraints: true`); an
  untouched flag defers to the config. Same rule `export` already applies
  to `--compress` (pflag `Changed`, not "differs from the default").
- Not `--check`: in v2 `check` is an alias of `validate` and
  `check-integrity` of `test` — both *verify*. This option *carries*.
  Nor is there anything to verify at export time: the rows already passed
  the source's own CHECKs.
- **v2 only.** 1.x is frozen for new flags; new behaviour lands in v2.

**Import recreates only under `--strict-schema`.** That flag already means
"reproduce the source schema, and fail loudly where the data does not fit
it" — NOT NULL, UNIQUE and CHECK are the same kind of thing as
`VARCHAR(n)`. No second switch. Default import is unchanged (commit
`7887de4`).

**A constraint travels only while the rows it describes are unchanged in
shape.** Every path that reshapes data has to revise what it carries:

- **`pipeline` does not carry source constraints at all.** Its output is a
  query over a workspace — joins, aggregates, computed columns — and a CHECK
  on a source table says nothing about a `SUM` or a joined row. Carrying
  them would be a false statement. (This corrects the earlier sketch that
  listed `pipeline` alongside `export`.)
- **Column projection** (`--fields` on export, `to-tdtp`): facets on kept
  columns stay; a key or UNIQUE that names a dropped column is dropped
  (uniqueness of `(A, B)` says nothing about `A` alone); a raw `<Check>`
  that names a dropped column is dropped. The `xs:keyref` of §3 would
  reject a packet that forgot to.
- **Row filtering** (`--where`, `--limit`): a subset of valid rows is still
  valid — everything stays.
- **`merge`**: facets and NOT NULL survive (every input row satisfied them);
  **PK and UNIQUE do not** — a union of two valid packets can repeat a
  unique value. Drop them, or re-verify and drop on failure; never pass them
  through unchecked.

## 7. The prototype

```
tdtp-constraints-probe --dsn "server=...;database=...;encrypt=disable" --table dbo.Employees
```

Prints the `<Schema>` this draft would produce, then what it could not
express and why (raw CHECKs, expression defaults, case-insensitive patterns,
untrusted and disabled CHECKs, filtered unique indexes). Read-only: catalog
views only (`sys.columns`, `sys.indexes`/`sys.index_columns`,
`sys.check_constraints`, `sys.default_constraints`,
`sys.extended_properties`).

Tests: `checkparse_test.go` holds the stored forms above as fixtures;
`constraints_integration_test.go` builds a table on a live server and checks
the catalog reading end to end (skips without one — `MSSQL_TEST_DSN_PROD`).

## 8. Before deciding

1. **Run the probe on the real HR schema** and count: CHECKs recognized vs
   raw, how many columns are NOT NULL, composite keys whose order differs from
   column order, patterns with letters under `_CI_`. The probe table was
   written to exercise the parser; a real schema says whether phase 2 is worth
   its cost.
2. **Versioning.** The current XSD has no `anyAttribute` on `FieldType`, so a
   packet carrying any of this is INVALID for every existing validator. That
   is a protocol version bump and an entry in the feature → version table,
   with `BumpVersion` when a producer writes constraints. The v1.4 Schema hash
   covers the new content without change.
3. **Other sources.** PostgreSQL has real regex in CHECKs and domains, MySQL
   8 enforces CHECK since 8.0.16, SQLite keeps the CHECK text only in the
   table DDL. Phase 1 maps everywhere; phase 2 extraction is per engine.
