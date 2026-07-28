# Migration Guide

Schema changes are **versioned SQL** run by `golang-migrate`, driven through
`cmd/migrate`.

## GORM AutoMigrate is not used. Ever.

`AutoMigrate` cannot express a backfill, will not drop or rename a column safely,
produces no reviewable diff, and makes the schema a side effect of whichever
struct definitions happen to be compiled into the running binary. Two replicas
on different versions would fight over the schema at startup.

Versioned SQL is the source of truth. `module.Migrator` exists only for tests.

---

## 1. Commands

Run through Make (which wraps `go run ./cmd/migrate`):

```bash
make migrate-create NAME=create_products_table
```

```bash
make migrate-up
```

```bash
make migrate-down STEPS=1
```

```bash
make migrate-version
```

| Command                             | Purpose                                     |
| ----------------------------------- | ------------------------------------------- |
| `migrate-create NAME=x`             | Scaffold an up/down pair.                    |
| `migrate-up`                        | Apply all pending migrations.                |
| `migrate-down STEPS=n`              | Roll back n migrations (default 1).          |
| `migrate-reset`                     | Roll back everything. Destructive.           |
| `migrate-version`                   | Print version; **exits 1 if dirty**.         |
| `migrate-force VERSION=n`           | Clear a dirty state after manual repair.     |

`migrate-version` exiting non-zero on a dirty schema lets CI fail a deploy
before it starts.

The CLI uses the same Viper config, DSN and Zap logger as the API, so there is no
second place where connection settings can drift.

---

## 2. File naming

```
{utc_timestamp}_{snake_case_description}.up.sql
{utc_timestamp}_{snake_case_description}.down.sql
```

```
20260722143000_create_products_table.up.sql
20260722143000_create_products_table.down.sql
```

Timestamps rather than sequential integers: two developers adding a migration on
separate branches would both claim number `7` and collide at merge time.

**Every `.up.sql` requires a matching `.down.sql`.** A migration that cannot be
reversed cannot be deployed on a Friday.

---

## 3. Docker

`cmd/migrate` is a **separate binary and a separate image** (`target: migrate` in
the Dockerfile). The API image ships no binary capable of altering the schema.

In Compose, migrations run to completion before the API starts:

```yaml
api:
  depends_on:
    migrate:
      condition: service_completed_successfully
```

The migrate service sets `restart: "no"`. A retry loop on a failing migration
would repeatedly re-attempt a schema change already diagnosed as broken.

In Kubernetes, run the same image as an **init container** or a pre-deploy Job.

---

## 4. The dirty flag

If a migration fails part-way, `golang-migrate` marks the schema **dirty** and
refuses to proceed. This is correct: it does not know what actually landed, and
guessing corrupts data.

Recovery is deliberately manual:

1. Inspect the database and determine what was actually applied.
2. Finish or undo the partial change by hand.
3. `make migrate-force VERSION=<the version that is truly applied>`.

`force` requires `-confirm` and runs no SQL — it only rewrites the version
record. It must never run automatically.

---

## 5. Writing safe migrations

### One logical change per migration

Easier to review, easier to roll back, and a failure has a smaller blast radius.

### Never edit an applied migration

If it has run anywhere but your own machine, write a new one. Editing changes
the checksum and desynchronises every environment that already ran it.

### Adding a NOT NULL column takes three migrations

```sql
-- 1. add nullable
ALTER TABLE products ADD COLUMN category_id UUID;

-- 2. backfill (separate migration)
UPDATE products SET category_id = '...' WHERE category_id IS NULL;

-- 3. add the constraint (separate migration)
ALTER TABLE products ALTER COLUMN category_id SET NOT NULL;
```

Doing it in one step holds an `ACCESS EXCLUSIVE` lock for the length of the
backfill — on a large table that is a write outage.

### Index large tables concurrently

```sql
CREATE INDEX CONCURRENTLY idx_products_company_sku ON products (company_id, sku);
```

A plain `CREATE INDEX` blocks writes for the duration.

`CONCURRENTLY` cannot run inside a transaction. `golang-migrate` wraps each
migration in one by default, so such a migration must be its own file with no
other statements.

### Expand/contract for renames

Never `ALTER TABLE ... RENAME COLUMN` in a single deploy: the old code is still
running during a rolling deploy and will query the old name.

1. Add the new column; write to both.
2. Backfill.
3. Deploy code reading the new column.
4. Drop the old column in a later release.

---

## 6. Multi-tenancy rules

This is a shared-schema multi-tenant system. Every tenant-owned table:

```sql
CREATE TABLE products (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id),
    sku        CITEXT NOT NULL,
    ...
);

-- company_id FIRST in every index. Queries always filter on it, and a
-- composite index is only usable when the leading column is constrained.
CREATE INDEX idx_products_company ON products (company_id);

-- Uniqueness is per-tenant, never global. Two companies may both use SKU
-- "ABC-1"; a global unique constraint would let one tenant's data block
-- another's.
CREATE UNIQUE INDEX idx_products_company_sku ON products (company_id, sku)
    WHERE deleted_at IS NULL;
```

The partial `WHERE deleted_at IS NULL` allows a SKU to be reused after the
original row is soft-deleted.

---

## 7. Conventions

- **UUID primary keys**, `gen_random_uuid()` from `pgcrypto`. A WMS syncs records
  from offline mobile clients, and a client that cannot mint its own identifiers
  must round-trip to the server before it can reference anything it just created.
- **`CITEXT`** for case-insensitive values (SKU codes, emails), so `ABC-1` and
  `abc-1` are not two rows.
- **Soft deletes** via `deleted_at TIMESTAMPTZ`. A WMS must retain history: a
  deleted product still has to resolve on last year's stock movements.
- **`TIMESTAMPTZ`, never `TIMESTAMP`.** Warehouses span time zones.
- **Foreign keys are declared.** Application-level integrity is not integrity.

---

## 8. Baseline

`20260101000000_enable_extensions` enables `pgcrypto` and `citext`. It creates no
business tables — it is infrastructure that every later migration depends on.
