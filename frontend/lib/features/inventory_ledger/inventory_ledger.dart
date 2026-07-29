import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/constants/app_spacing.dart';
import '../../shared/widgets/buttons/app_button.dart';
import '../../shared/widgets/filters/date_range_filter.dart';
import '../auth/presentation/controllers/auth_controller.dart';
import '../auth/presentation/controllers/permission_controller.dart';
import '../inventory/inventory.dart' show InventoryLookupRef;
import '../lookups/domain/entities/lookup_item.dart';
import '../lookups/domain/repositories/lookup_repository.dart';
import '../lookups/presentation/widgets/app_lookup_dropdown.dart';
import '../master/presentation/pages/master_detail_page.dart';
import '../master/presentation/pages/master_list_page.dart';
import '../master/presentation/widgets/master_states.dart';
import '../master/presentation/widgets/master_toolbar.dart';

/// The ledger is append-only and read-only: there is exactly one capability to
/// hold, because there is no operation that changes anything.
abstract final class InventoryLedgerPermissions {
  static const read = 'inventoryledger.read';
}

/// Sort keys the backend allows, aligned to the visible columns.
const _sortableColumns = [
  'occurred_at',
  'movement_type',
  '', // Product
  '', // Warehouse
  '', // Reference
  '', // User
  '', // Before
  '', // Delta
  '', // After
  '', // Actions
];

void _ledgerColumnSort(int columnIndex, bool ascending) {}

/// The movement types the ledger records. The list mirrors the backend's closed
/// enum; an unknown value would be rejected by the query validator.
const _movementTypes = [
  'INITIAL_BALANCE',
  'INBOUND',
  'OUTBOUND',
  'TRANSFER',
  'RESERVATION',
  'ALLOCATION',
  'ADJUSTMENT',
  'QUARANTINE',
  'CYCLE_COUNT',
];

// ---------- Model ----------

/// A four-balance snapshot plus its derived total.
class BucketSnapshot {
  const BucketSnapshot({
    required this.available,
    required this.reserved,
    required this.allocated,
    required this.quarantined,
    required this.onHand,
  });
  final int available, reserved, allocated, quarantined, onHand;

  factory BucketSnapshot.fromJson(Map<String, dynamic> json) => BucketSnapshot(
    available: json['available'] as int,
    reserved: json['reserved'] as int,
    allocated: json['allocated'] as int,
    quarantined: json['quarantined'] as int,
    onHand: json['on_hand'] as int,
  );
}

/// One immutable ledger entry: what moved, from which balances to which, under
/// whose hand, and against which document.
class LedgerEntry {
  const LedgerEntry({
    required this.id,
    required this.positionId,
    required this.productId,
    required this.warehouseId,
    required this.locationId,
    required this.movementType,
    required this.actorId,
    required this.before,
    required this.after,
    required this.delta,
    required this.occurredAt,
    this.lotNumber = '',
    this.serialNumber = '',
    this.referenceType = '',
    this.referenceId = '',
    this.documentNumber = '',
    this.reason = '',
  });
  final String id, positionId, productId, warehouseId, locationId;
  final String movementType, actorId, occurredAt;
  final String lotNumber, serialNumber;
  final String referenceType, referenceId, documentNumber, reason;
  final BucketSnapshot before, after, delta;

  /// What a reader recognises the movement by: the document number when there is
  /// one, otherwise the reference type.
  String get reference => documentNumber.isNotEmpty
      ? documentNumber
      : (referenceType.isNotEmpty ? referenceType : '—');

  factory LedgerEntry.fromJson(Map<String, dynamic> json) => LedgerEntry(
    id: json['id'] as String,
    positionId: json['position_id'] as String,
    productId: json['product_id'] as String,
    warehouseId: json['warehouse_id'] as String,
    locationId: json['location_id'] as String,
    movementType: json['movement_type'] as String,
    actorId: json['actor_id'] as String,
    lotNumber: json['lot_number'] as String? ?? '',
    serialNumber: json['serial_number'] as String? ?? '',
    referenceType: json['reference_type'] as String? ?? '',
    referenceId: json['reference_id'] as String? ?? '',
    documentNumber: json['document_number'] as String? ?? '',
    reason: json['reason'] as String? ?? '',
    before: BucketSnapshot.fromJson(json['before'] as Map<String, dynamic>),
    after: BucketSnapshot.fromJson(json['after'] as Map<String, dynamic>),
    delta: BucketSnapshot.fromJson(json['delta'] as Map<String, dynamic>),
    occurredAt: json['occurred_at'] as String? ?? '',
  );
}

/// One server page of ledger entries plus the server-reported total.
class LedgerPage {
  const LedgerPage(this.items, this.total);
  final List<LedgerEntry> items;
  final int total;
}

// ---------- Data ----------

/// Read-only by construction: the ledger exposes no write endpoint, so this
/// repository offers no create, update or delete.
class InventoryLedgerRepository {
  InventoryLedgerRepository(this._api);
  final ApiClient _api;

  Future<LedgerPage> list({
    String productId = '',
    String warehouseId = '',
    String locationId = '',
    String positionId = '',
    String movementType = '',
    String occurredFrom = '',
    String occurredTo = '',
    String sort = 'occurred_at',
    String order = 'desc',
    int page = 1,
    int limit = 10,
  }) async {
    final response = await _api.dio.get(
      '/inventory-ledger',
      queryParameters: {
        'page': page,
        'limit': limit,
        'sort': sort,
        'order': order,
        if (productId.isNotEmpty) 'product_id': productId,
        if (warehouseId.isNotEmpty) 'warehouse_id': warehouseId,
        if (locationId.isNotEmpty) 'location_id': locationId,
        if (positionId.isNotEmpty) 'position_id': positionId,
        if (movementType.isNotEmpty) 'movement_type': movementType,
        if (occurredFrom.isNotEmpty) 'occurred_from': occurredFrom,
        if (occurredTo.isNotEmpty) 'occurred_to': occurredTo,
      },
    );
    final meta = response.data['meta']['pagination'] as Map<String, dynamic>;
    return LedgerPage(
      (response.data['data'] as List)
          .map((item) => LedgerEntry.fromJson(item as Map<String, dynamic>))
          .toList(),
      meta['total'] as int,
    );
  }

  Future<LedgerEntry> get(String id) async => LedgerEntry.fromJson(
    (await _api.dio.get('/inventory-ledger/$id')).data['data']
        as Map<String, dynamic>,
  );
}

final inventoryLedgerRepositoryProvider = Provider(
  (ref) => InventoryLedgerRepository(ref.read(apiClientProvider)),
);

class LedgerListQuery {
  const LedgerListQuery({
    this.productId = '',
    this.warehouseId = '',
    this.positionId = '',
    this.movementType = '',
    this.occurredFrom = '',
    this.occurredTo = '',
    this.sort = 'occurred_at',
    this.ascending = false,
    this.page = 0,
  });
  final String productId, warehouseId, positionId, movementType;
  final String occurredFrom, occurredTo, sort;
  final bool ascending;
  final int page;

  LedgerListQuery copyWith({
    String? productId,
    String? warehouseId,
    String? positionId,
    String? movementType,
    String? occurredFrom,
    String? occurredTo,
    String? sort,
    bool? ascending,
    int? page,
  }) => LedgerListQuery(
    productId: productId ?? this.productId,
    warehouseId: warehouseId ?? this.warehouseId,
    positionId: positionId ?? this.positionId,
    movementType: movementType ?? this.movementType,
    occurredFrom: occurredFrom ?? this.occurredFrom,
    occurredTo: occurredTo ?? this.occurredTo,
    sort: sort ?? this.sort,
    ascending: ascending ?? this.ascending,
    page: page ?? this.page,
  );

  @override
  bool operator ==(Object other) =>
      other is LedgerListQuery &&
      other.productId == productId &&
      other.warehouseId == warehouseId &&
      other.positionId == positionId &&
      other.movementType == movementType &&
      other.occurredFrom == occurredFrom &&
      other.occurredTo == occurredTo &&
      other.sort == sort &&
      other.ascending == ascending &&
      other.page == page;
  @override
  int get hashCode => Object.hash(
    productId,
    warehouseId,
    positionId,
    movementType,
    occurredFrom,
    occurredTo,
    sort,
    ascending,
    page,
  );
}

final ledgerListProvider = FutureProvider.autoDispose
    .family<LedgerPage, LedgerListQuery>(
      (ref, query) => ref
          .read(inventoryLedgerRepositoryProvider)
          .list(
            productId: query.productId,
            warehouseId: query.warehouseId,
            positionId: query.positionId,
            movementType: query.movementType,
            occurredFrom: query.occurredFrom,
            occurredTo: query.occurredTo,
            sort: query.sort,
            order: query.ascending ? 'asc' : 'desc',
            page: query.page + 1,
          ),
    );

final ledgerDetailProvider = FutureProvider.autoDispose
    .family<LedgerEntry, String>(
      (ref, id) => ref.read(inventoryLedgerRepositoryProvider).get(id),
    );

// ---------- List ----------

class InventoryLedgerListPage extends ConsumerStatefulWidget {
  const InventoryLedgerListPage({super.key, this.positionId});

  /// When set, the page opens scoped to one position's history.
  final String? positionId;

  @override
  ConsumerState<InventoryLedgerListPage> createState() =>
      _InventoryLedgerListPageState();
}

class _InventoryLedgerListPageState
    extends ConsumerState<InventoryLedgerListPage> {
  late LedgerListQuery _query = LedgerListQuery(
    positionId: widget.positionId ?? '',
  );
  LookupItem? _product, _warehouse;
  DateTimeRange? _range;

  void _refresh() => ref.invalidate(ledgerListProvider);

  void _update(LedgerListQuery next) => setState(() => _query = next);

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(ledgerListProvider(_query));
    final products = ref.labelsFor(LookupType.products);
    final warehouses = ref.labelsFor(LookupType.warehouses);

    return PermissionGuard(
      permission: InventoryLedgerPermissions.read,
      child: state.when(
        loading: () => const MasterLoading(),
        error: (error, stack) =>
            MasterErrorState(message: '$error', onRetry: _refresh),
        data: (result) => MasterListPage<String>(
          title: 'Inventory ledger',
          currentPage: _query.page,
          totalRecords: result.total,
          onPageChanged: (value) => _update(_query.copyWith(page: value)),
          sortField: _query.sort,
          sortDirection: _query.ascending,
          sortFields: _sortableColumns,
          onSortChanged: (field, ascending) => _update(
            _query.copyWith(sort: field, ascending: ascending, page: 0),
          ),
          // The ledger is append-only: no create, edit or delete action exists.
          actions: [
            MasterToolbar(
              trailing: [
                AppButton(
                  label: 'Refresh',
                  icon: Icons.refresh,
                  isOutlined: true,
                  onPressed: _refresh,
                ),
              ],
            ),
          ],
          filters: _LedgerFilters(
            product: _product,
            warehouse: _warehouse,
            movementType: _query.movementType,
            range: _range,
            onProduct: (value) {
              setState(() => _product = value);
              _update(_query.copyWith(productId: value?.id ?? '', page: 0));
            },
            onWarehouse: (value) {
              setState(() => _warehouse = value);
              _update(_query.copyWith(warehouseId: value?.id ?? '', page: 0));
            },
            onMovementType: (value) =>
                _update(_query.copyWith(movementType: value ?? '', page: 0)),
            onRange: (value) {
              setState(() => _range = value);
              _update(
                _query.copyWith(
                  // The API's range is half-open, so the exclusive upper bound
                  // is the day after the one the user picked.
                  occurredFrom: value == null ? '' : _isoDate(value.start),
                  occurredTo: value == null
                      ? ''
                      : _isoDate(value.end.add(const Duration(days: 1))),
                  page: 0,
                ),
              );
            },
            onReset: () {
              setState(() {
                _product = null;
                _warehouse = null;
                _range = null;
              });
              _update(LedgerListQuery(positionId: widget.positionId ?? ''));
            },
            onApply: _refresh,
          ),
          columns: const [
            DataColumn(label: Text('Occurred at'), onSort: _ledgerColumnSort),
            DataColumn(label: Text('Movement'), onSort: _ledgerColumnSort),
            DataColumn(label: Text('Product')),
            DataColumn(label: Text('Warehouse')),
            DataColumn(label: Text('Reference')),
            DataColumn(label: Text('User')),
            DataColumn(label: Text('Before'), numeric: true),
            DataColumn(label: Text('Delta'), numeric: true),
            DataColumn(label: Text('After'), numeric: true),
            DataColumn(label: Text('Actions')),
          ],
          rows: [
            for (final item in result.items)
              DataRow(
                cells: [
                  DataCell(Text(_formatTimestamp(item.occurredAt))),
                  DataCell(Text(item.movementType)),
                  DataCell(Text(_label(products, item.productId))),
                  DataCell(Text(_label(warehouses, item.warehouseId))),
                  DataCell(Text(item.reference)),
                  DataCell(Text(_shortId(item.actorId))),
                  DataCell(Text('${item.before.onHand}')),
                  DataCell(_DeltaText(value: item.delta.onHand)),
                  DataCell(Text('${item.after.onHand}')),
                  DataCell(
                    IconButton(
                      tooltip: 'View',
                      icon: const Icon(Icons.visibility_outlined),
                      onPressed: () => Navigator.push(
                        context,
                        MaterialPageRoute(
                          builder: (_) =>
                              InventoryLedgerDetailPage(id: item.id),
                        ),
                      ),
                    ),
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }
}

class _LedgerFilters extends StatelessWidget {
  const _LedgerFilters({
    required this.product,
    required this.warehouse,
    required this.movementType,
    required this.range,
    required this.onProduct,
    required this.onWarehouse,
    required this.onMovementType,
    required this.onRange,
    required this.onReset,
    required this.onApply,
  });
  final LookupItem? product, warehouse;
  final String movementType;
  final DateTimeRange? range;
  final ValueChanged<LookupItem?> onProduct, onWarehouse;
  final ValueChanged<String?> onMovementType;
  final ValueChanged<DateTimeRange?> onRange;
  final VoidCallback onReset, onApply;

  @override
  Widget build(BuildContext context) => Wrap(
    spacing: AppSpacing.sm,
    runSpacing: AppSpacing.sm,
    crossAxisAlignment: WrapCrossAlignment.center,
    children: [
      SizedBox(
        width: 240,
        child: AppLookupDropdown(
          type: LookupType.products,
          label: 'Product',
          value: product,
          onChanged: onProduct,
        ),
      ),
      SizedBox(
        width: 220,
        child: AppLookupDropdown(
          type: LookupType.warehouses,
          label: 'Warehouse',
          value: warehouse,
          onChanged: onWarehouse,
        ),
      ),
      DropdownButton<String?>(
        value: movementType.isEmpty ? null : movementType,
        hint: const Text('Movement type'),
        items: [
          const DropdownMenuItem<String?>(value: null, child: Text('All')),
          for (final type in _movementTypes)
            DropdownMenuItem<String?>(value: type, child: Text(type)),
        ],
        onChanged: onMovementType,
      ),
      DateRangeFilter(value: range, onChanged: onRange),
      TextButton(onPressed: onReset, child: const Text('Reset')),
      AppButton(label: 'Apply', onPressed: onApply),
    ],
  );
}

class _DeltaText extends StatelessWidget {
  const _DeltaText({required this.value});
  final int value;
  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Text(
      value > 0 ? '+$value' : '$value',
      style: TextStyle(
        fontWeight: FontWeight.w600,
        color: value == 0
            ? null
            : (value > 0 ? colors.primary : colors.error),
      ),
    );
  }
}

// ---------- Detail ----------

class InventoryLedgerDetailPage extends ConsumerWidget {
  const InventoryLedgerDetailPage({super.key, required this.id});
  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) => PermissionGuard(
    permission: InventoryLedgerPermissions.read,
    child: ref
        .watch(ledgerDetailProvider(id))
        .when(
          loading: () => const MasterLoading(),
          error: (error, stack) => MasterErrorState(
            message: '$error',
            onRetry: () => ref.invalidate(ledgerDetailProvider(id)),
          ),
          data: (item) => _buildDetail(context, ref, item),
        ),
  );

  Widget _buildDetail(BuildContext context, WidgetRef ref, LedgerEntry item) {
    final products = ref.labelsFor(LookupType.products);
    final warehouses = ref.labelsFor(LookupType.warehouses);
    final locations = ref.labelsFor(LookupType.locations);

    return MasterDetailPage(
      title: '${item.movementType} · ${_formatTimestamp(item.occurredAt)}',
      // Read-only: the ledger records history and can never be edited.
      actions: const [],
      general: [
        ListTile(
          title: const Text('Product'),
          subtitle: Text(_label(products, item.productId)),
        ),
        ListTile(
          title: const Text('Warehouse'),
          subtitle: Text(_label(warehouses, item.warehouseId)),
        ),
        ListTile(
          title: const Text('Location'),
          subtitle: Text(_label(locations, item.locationId)),
        ),
        if (item.lotNumber.isNotEmpty)
          ListTile(
            title: const Text('Lot number'),
            subtitle: Text(item.lotNumber),
          ),
        if (item.serialNumber.isNotEmpty)
          ListTile(
            title: const Text('Serial number'),
            subtitle: Text(item.serialNumber),
          ),
        ListTile(
          title: const Text('Reference'),
          subtitle: Text(item.reference),
        ),
        if (item.reason.isNotEmpty)
          ListTile(title: const Text('Reason'), subtitle: Text(item.reason)),
        const Divider(),
        const ListTile(title: Text('Balances')),
        _BucketTable(entry: item),
      ],
      audit: [
        ListTile(
          title: const Text('Recorded by'),
          subtitle: Text(item.actorId),
        ),
        ListTile(
          title: const Text('Occurred at'),
          subtitle: Text(item.occurredAt),
        ),
        ListTile(
          title: const Text('Position'),
          subtitle: Text(item.positionId),
        ),
      ],
    );
  }
}

class _BucketTable extends StatelessWidget {
  const _BucketTable({required this.entry});
  final LedgerEntry entry;

  @override
  Widget build(BuildContext context) => SingleChildScrollView(
    scrollDirection: Axis.horizontal,
    child: DataTable(
      columns: const [
        DataColumn(label: Text('Bucket')),
        DataColumn(label: Text('Before'), numeric: true),
        DataColumn(label: Text('Delta'), numeric: true),
        DataColumn(label: Text('After'), numeric: true),
      ],
      rows: [
        _row('Available', entry.before.available, entry.delta.available,
            entry.after.available),
        _row('Reserved', entry.before.reserved, entry.delta.reserved,
            entry.after.reserved),
        _row('Allocated', entry.before.allocated, entry.delta.allocated,
            entry.after.allocated),
        _row('Quarantined', entry.before.quarantined, entry.delta.quarantined,
            entry.after.quarantined),
        _row('On hand', entry.before.onHand, entry.delta.onHand,
            entry.after.onHand),
      ],
    ),
  );

  DataRow _row(String label, int before, int delta, int after) => DataRow(
    cells: [
      DataCell(Text(label)),
      DataCell(Text('$before')),
      DataCell(_DeltaText(value: delta)),
      DataCell(Text('$after')),
    ],
  );
}

// ---------- Shared helpers ----------

String _label(Map<String, String> labels, String id) =>
    labels[id] ?? _shortId(id);

String _shortId(String id) => id.length > 8 ? '${id.substring(0, 8)}…' : id;

String _formatTimestamp(String value) => value.length >= 16
    ? value.substring(0, 16).replaceFirst('T', ' ')
    : value;

String _isoDate(DateTime value) =>
    '${value.year.toString().padLeft(4, '0')}-'
    '${value.month.toString().padLeft(2, '0')}-'
    '${value.day.toString().padLeft(2, '0')}';
