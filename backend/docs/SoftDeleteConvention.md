# Soft Delete Convention

Deleting a record sets `deleted_at` instead of removing the row. This document
explains why, how restore works, and when a hard delete is legitimate.

---

## 1. Why soft delete

### Warehouse data has referential history

A product deleted today still appears on last year's stock movements, goods
receipts and pick lists. Hard-deleting it either breaks those foreign keys or
cascades and destroys the movement history along with it — and that history is
what an auditor, a stock reconciliation and a customer dispute all depend on.

A row that still exists with `deleted_at` set keeps every historical join
resolving while disappearing from every operational query.

### Deletion is usually a mistake

In a warehouse, "delete this product" is often a mis-tap on a scanner while
someone is holding a box. Recovering from that with a soft delete is an `UPDATE`;
recovering from a hard delete is a restore from backup, which means losing every
change since the last snapshot across the whole tenant.

### Multi-tenant blast radius

One tenant's destructive mistake must never require an operation that affects
other tenants. Point-in-time recovery of a shared database does exactly that.

### The cost

Soft deletes are not free, and the trade is deliberate:

- Every query carries `deleted_at IS NULL`. GORM adds it automatically, which is
  precisely why `BaseEntity` uses `gorm.DeletedAt` rather than a `*time.Time`.
- Indexes must account for it, or the planner scans deleted rows to discard them.
- Unique constraints must be partial, or a deleted record blocks the reuse of its
  own SKU.
- The table grows forever without a purge policy. See section 4.

---

## 2. How it works

`entity.BaseEntity` carries:

```go
DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
```

That one field changes the behaviour of every query against the model:

| Operation                          | Resulting SQL                                    |
| ---------------------------------- | ------------------------------------------------ |
| `repo.Delete(ctx, id, scope)`      | `UPDATE ... SET deleted_at = now() WHERE ...`     |
| `repo.FindByID(...)`               | `SELECT ... WHERE ... AND deleted_at IS NULL`     |
| `repo.FindAll(...)`                | `SELECT ... WHERE ... AND deleted_at IS NULL`     |
| `repo.Count(...)`                  | `SELECT count(*) ... AND deleted_at IS NULL`      |
| `... , repository.WithDeleted()`   | filter omitted                                    |
| `... , repository.OnlyDeleted()`   | `WHERE deleted_at IS NOT NULL`                    |
| `repo.HardDelete(ctx, id, scope)`  | `DELETE FROM ...`                                 |

`json:"-"` keeps deletion state out of the API. An endpoint either returns a row
or it does not; exposing `deleted_at` invites clients to build their own
filtering and get it wrong.

Deleting an already-deleted row returns `NOT_FOUND` rather than succeeding
silently, because `RowsAffected` is zero.

---

## 3. Restore strategy

**No restore endpoint is implemented.** The mechanism is documented so that when
one is built it is built the same way everywhere.

### Migrations must make restore possible

A unique constraint that ignores `deleted_at` makes restore fail — and, worse,
blocks the tenant from reusing the SKU of a product they deleted:

```sql
-- WRONG: a deleted product permanently reserves its SKU
CREATE UNIQUE INDEX ux_products_sku ON products (company_id, sku);

-- RIGHT: the constraint applies only to live rows
CREATE UNIQUE INDEX ux_products_sku ON products (company_id, sku)
    WHERE deleted_at IS NULL;
```

The partial index is what lets a tenant delete `ABC-1` and create a new `ABC-1`
immediately. It also means restore can **conflict**: if a new `ABC-1` exists,
restoring the old one violates uniqueness. A restore flow must detect that and
report a `CONFLICT`, not fail with a 500.

### Shape of a restore

```go
func (s *Service) Restore(ctx context.Context, id uuid.UUID) error {
    companyID, err := tenant(ctx)
    if err != nil {
        return err
    }

    return s.tx.RunInTransaction(ctx, func(ctx context.Context) error {
        // Must opt into deleted rows; the default query cannot see it.
        product, err := s.repo.FindDeletedByID(ctx, companyID, id)
        if err != nil {
            return err
        }

        // Restoring must not collide with a record created since.
        taken, err := s.repo.ExistsBySKU(ctx, companyID, product.SKU)
        if err != nil {
            return err
        }
        if taken {
            return apperror.Conflict("Another product already uses this SKU")
        }

        return s.repo.Restore(ctx, companyID, id)
    })
}
```

Rules for any future restore:

1. **Tenant-scoped** like every other operation.
2. **Inside a transaction** — the uniqueness check and the update must be atomic.
3. **Conflict-aware** — report `CONFLICT`, do not let the index raise a 500.
4. **Audited** — restore is a privileged action and must be logged with the
   principal who performed it.
5. **Not cascading.** Restoring a product must not resurrect its stock levels;
   those quantities were correct at deletion time and are stale now.

---

## 4. Purge strategy

Soft-deleted rows accumulate forever unless something removes them. A purge is a
**hard delete**, and it is the only legitimate use of `HardDelete`.

### When purging is required

| Trigger                | Rule                                                       |
| ---------------------- | ---------------------------------------------------------- |
| Retention expiry       | Rows soft-deleted beyond the retention window.              |
| GDPR erasure           | A subject's personal data, on a verified request.           |
| Tenant offboarding     | Everything belonging to a closed company, after its grace period. |

Retention is a per-tenant policy, not a constant. A logistics customer may be
legally required to keep movement records for seven years, so a global "purge
after 90 days" would delete data they are obliged to retain.

### How a purge job must behave

- **Run as a background job, never in an HTTP request.** It is long, it is
  destructive, and it must not be triggerable by a timed-out client retrying.
- **Delete in bounded batches** (a few thousand rows) with a pause between them.
  One `DELETE` over millions of rows holds locks and bloats WAL long enough to
  stall replication.
- **Respect foreign keys.** Purge children before parents, or the delete fails
  part-way and leaves the batch half-applied.
- **Never purge a row still referenced by a live record.** A product referenced
  by a live stock movement must be retained even past its retention window.
- **Log every purge** — table, tenant, row count, criteria. A hard delete leaves
  no other evidence it happened.
- **Be resumable.** Batch by `deleted_at` ascending so an interrupted run
  continues where it stopped rather than restarting.

### Not implemented yet

No purge job exists. Nothing is currently deleted permanently, which is the safe
default for a system with no customers yet. Before the first production tenant,
the retention policy has to be decided per table — that is a product and legal
decision, not an engineering one, and guessing it in code would be worse than
leaving it explicit here.

---

## 5. Rules

- Ordinary deletion is **always** soft. `HardDelete` is never reachable from
  HTTP.
- Unique constraints on soft-deletable tables are **partial**:
  `WHERE deleted_at IS NULL`.
- Composite indexes lead with `company_id`; add `deleted_at` where the planner
  benefits.
- `WithDeleted()` and `OnlyDeleted()` are rare and deliberate. A module using
  them routinely has probably modelled a status field as a deletion.
- **Deletion is not a status.** "Archived", "discontinued" and "inactive" are
  business states with their own rules and belong in a `status` column.
  `deleted_at` means "this record was removed in error or is no longer real",
  nothing else.

---

## 6. Checklist for a soft-deletable entity

- [ ] Embeds `entity.BaseEntity`.
- [ ] Unique indexes are partial on `deleted_at IS NULL`.
- [ ] `deleted_at` indexed if the table is large.
- [ ] Business states modelled as `status`, not as deletion.
- [ ] Retention period documented if the table holds personal data.
- [ ] Restore path considered — even if no endpoint exists yet.
