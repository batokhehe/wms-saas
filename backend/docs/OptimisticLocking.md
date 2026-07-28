# Optimistic locking

## Problem: lost updates

A read-modify-write flow can race even when each request uses a transaction.
Two requests can read the same warehouse or storage location, both apply valid
domain operations, and the later write can silently erase the earlier one. That
is a lost update. Row locks are deliberately not used here: this platform must
remain horizontally scalable and routine aggregate edits should not block each
other.

## Version lifecycle

`entity.BaseEntity` owns an unsigned `Version` column. New rows start at `1`;
the migration backfills every existing BaseEntity row to `1`. Domain aggregates
hold a read-only copy so a repository can use it as the expected value. They
have no Version mutator: version progression is persistence metadata, not a
business transition.

## Repository behaviour

Warehouse and StorageLocation repositories use `Base.UpdateOptimistic`. It
issues one SQL update guarded by `id` and `version`, writes all aggregate fields
and increments `version` in that same statement. One competing update changes
the row from N to N+1. A later update still expecting N affects zero rows and
returns `repository.ErrConcurrentModification`; it never retries or overwrites.

Services turn that sentinel into the existing HTTP `409 CONFLICT` envelope.
Its details include `entity_id` and `current_version`, obtained after the failed
transaction rolls back. API request and success-response contracts remain
unchanged.

## Adding Inventory

Inventory aggregate persistence models should embed `BaseEntity`, map the
aggregate's read-only version through reconstitution, and call
`UpdateOptimistic` for every aggregate update. Do not use `SELECT FOR UPDATE`,
process-local locks, or a domain-level Version setter. The migration convention
is add nullable column, backfill `1`, add default `1`, then set `NOT NULL`.
