# Migrations

Versioned SQL is the source of truth for the database schema. GORM's
`AutoMigrate` is **not** used in staging or production: it cannot express
backfills, it will not drop or rename safely, and it gives no reviewable diff.

## Naming

```
{version}_{description}.up.sql
{version}_{description}.down.sql
```

Version is a UTC timestamp, e.g. `20260722120000_create_tenants_table.up.sql`.
Timestamps avoid the merge conflicts that sequential integers cause when two
branches add a migration at the same time.

Every `.up.sql` requires a matching `.down.sql`. A migration that cannot be
reversed is a migration that cannot be deployed on a Friday.

## Running

Using [golang-migrate](https://github.com/golang-migrate/migrate):

```bash
migrate -path ./migrations -database "$DATABASE_URL" up
```

## Rules

- One logical change per migration.
- Never edit a migration that has been applied anywhere but your own machine;
  write a new one instead.
- Adding a `NOT NULL` column to a populated table needs three steps: add
  nullable, backfill, then add the constraint. A single-step version locks the
  table for the length of the backfill.
- Create indexes with `CONCURRENTLY` on large tables so writes are not blocked.
