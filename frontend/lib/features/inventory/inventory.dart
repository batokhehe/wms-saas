import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/constants/app_spacing.dart';
import '../../shared/widgets/buttons/app_button.dart';
import '../auth/presentation/controllers/auth_controller.dart';
import '../auth/presentation/controllers/permission_controller.dart';
import '../lookups/domain/entities/lookup_item.dart';
import '../lookups/domain/repositories/lookup_repository.dart';
import '../lookups/presentation/providers/lookup_provider.dart';
import '../lookups/presentation/widgets/app_lookup_dropdown.dart';
import '../master/presentation/pages/master_detail_page.dart';
import '../master/presentation/pages/master_form_page.dart';
import '../master/presentation/pages/master_list_page.dart';
import '../master/presentation/widgets/master_states.dart';
import '../master/presentation/widgets/master_toolbar.dart';

/// Permission codes this module's controls are gated on. They mirror the codes
/// the inventory routes enforce server-side: moving stock, promising it,
/// quarantining it and correcting it are deliberately separate capabilities.
abstract final class InventoryPermissions {
  static const read = 'inventory.read';
  static const create = 'inventory.create';
  static const update = 'inventory.update';
  static const reserve = 'inventory.reserve';
  static const lock = 'inventory.lock';
  static const transfer = 'inventory.transfer';
  static const adjust = 'inventory.adjust';
}

/// Sort keys the backend allows, aligned to the visible columns. Anything else
/// is rejected by the API's allow-list, so only these columns are sortable.
const _sortableColumns = [
  '', // Product
  '', // Warehouse
  '', // Location
  '', // Tracking
  'available',
  'reserved',
  '', // Allocated
  '', // Quarantined
  '', // On hand
  'updated_at',
  '', // Actions
];

void _positionColumnSort(int columnIndex, bool ascending) {}

// ---------- Model ----------

/// A stock position: what is where, split across the four balances.
///
/// On-hand is DERIVED by the server as the sum of the four buckets and is never
/// stored, so it is read here rather than recomputed.
class InventoryPosition {
  const InventoryPosition({
    required this.id,
    required this.warehouseId,
    required this.locationId,
    required this.productId,
    required this.tracking,
    required this.available,
    required this.reserved,
    required this.allocated,
    required this.quarantined,
    required this.onHand,
    required this.createdAt,
    required this.updatedAt,
    this.lotNumber = '',
    this.serialNumber = '',
  });
  final String id, warehouseId, locationId, productId, tracking;
  final int available, reserved, allocated, quarantined, onHand;
  final String createdAt, updatedAt, lotNumber, serialNumber;

  factory InventoryPosition.fromJson(Map<String, dynamic> json) =>
      InventoryPosition(
        id: json['id'] as String,
        warehouseId: json['warehouse_id'] as String,
        locationId: json['location_id'] as String,
        productId: json['product_id'] as String,
        tracking: json['tracking'] as String,
        lotNumber: json['lot_number'] as String? ?? '',
        serialNumber: json['serial_number'] as String? ?? '',
        available: json['available'] as int,
        reserved: json['reserved'] as int,
        allocated: json['allocated'] as int,
        quarantined: json['quarantined'] as int,
        onHand: json['on_hand'] as int,
        createdAt: json['created_at'] as String? ?? '',
        updatedAt: json['updated_at'] as String? ?? '',
      );
}

/// One server page of positions plus the server-reported total.
class InventoryPositionPage {
  const InventoryPositionPage(this.items, this.total);
  final List<InventoryPosition> items;
  final int total;
}

/// The stock key a receipt addresses. A position is opened on first receipt, so
/// receiving is addressed by key rather than by id.
class ReceiveStockInput {
  const ReceiveStockInput({
    required this.warehouseId,
    required this.locationId,
    required this.productId,
    required this.tracking,
    required this.quantity,
    this.lotNumber,
    this.serialNumber,
  });
  final String warehouseId, locationId, productId, tracking;
  final int quantity;
  final String? lotNumber, serialNumber;

  Map<String, dynamic> toJson() => {
    'warehouse_id': warehouseId,
    'location_id': locationId,
    'product_id': productId,
    'tracking': tracking,
    if (lotNumber != null && lotNumber!.isNotEmpty) 'lot_number': lotNumber,
    if (serialNumber != null && serialNumber!.isNotEmpty)
      'serial_number': serialNumber,
    'quantity': quantity,
  };
}

/// The quantity movements that act on an existing position. Each maps to one
/// endpoint and one permission.
enum StockMovement {
  issue('issue', 'Issue stock', InventoryPermissions.update),
  reserve('reserve', 'Reserve stock', InventoryPermissions.reserve),
  release('release', 'Release reservation', InventoryPermissions.reserve),
  allocate('allocate', 'Allocate stock', InventoryPermissions.reserve),
  deallocate('deallocate', 'Deallocate stock', InventoryPermissions.reserve),
  quarantine('quarantine', 'Move to quarantine', InventoryPermissions.lock),
  releaseQuarantine(
    'release-quarantine',
    'Release from quarantine',
    InventoryPermissions.lock,
  );

  const StockMovement(this.path, this.label, this.permission);
  final String path, label, permission;
}

/// Why an absolute correction was made. The reason is not decoration: a cycle
/// count and a shrinkage write-off move the same units but mean opposite things.
enum AdjustmentType {
  cycleCount('CYCLE_COUNT', 'Cycle count'),
  damage('DAMAGE', 'Damage'),
  shrinkage('SHRINKAGE', 'Shrinkage'),
  found('FOUND', 'Found'),
  initialBalance('INITIAL_BALANCE', 'Initial balance'),
  manualCorrection('MANUAL_CORRECTION', 'Manual correction');

  const AdjustmentType(this.code, this.label);
  final String code, label;
}

// ---------- Data ----------

class InventoryRepository {
  InventoryRepository(this._api);
  final ApiClient _api;

  Future<InventoryPositionPage> list({
    String warehouseId = '',
    String locationId = '',
    String productId = '',
    String tracking = '',
    String sort = 'updated_at',
    String order = 'desc',
    int page = 1,
    int limit = 10,
  }) async {
    final response = await _api.dio.get(
      '/inventory-positions',
      queryParameters: {
        'page': page,
        'limit': limit,
        'sort': sort,
        'order': order,
        if (warehouseId.isNotEmpty) 'warehouse_id': warehouseId,
        if (locationId.isNotEmpty) 'location_id': locationId,
        if (productId.isNotEmpty) 'product_id': productId,
        if (tracking.isNotEmpty) 'tracking': tracking,
      },
    );
    final meta = response.data['meta']['pagination'] as Map<String, dynamic>;
    return InventoryPositionPage(
      (response.data['data'] as List)
          .map(
            (item) => InventoryPosition.fromJson(item as Map<String, dynamic>),
          )
          .toList(),
      meta['total'] as int,
    );
  }

  Future<InventoryPosition> get(String id) async => InventoryPosition.fromJson(
    (await _api.dio.get('/inventory-positions/$id')).data['data']
        as Map<String, dynamic>,
  );

  Future<void> receive(ReceiveStockInput input) =>
      _api.dio.post('/inventory-positions/receive', data: input.toJson());

  Future<void> move(String id, StockMovement movement, int quantity) =>
      _api.dio.post(
        '/inventory-positions/$id/${movement.path}',
        data: {'position_id': id, 'quantity': quantity},
      );

  Future<void> transfer(
    String id, {
    required String toWarehouseId,
    required String toLocationId,
    required int quantity,
  }) => _api.dio.post(
    '/inventory-positions/$id/transfer',
    data: {
      'from_position_id': id,
      'to_warehouse_id': toWarehouseId,
      'to_location_id': toLocationId,
      'quantity': quantity,
    },
  );

  Future<void> adjust(
    String id, {
    required int quantity,
    required AdjustmentType type,
    required String reason,
  }) => _api.dio.post(
    '/inventory-positions/$id/adjust',
    data: {
      'position_id': id,
      'quantity': quantity,
      'type': type.code,
      if (reason.isNotEmpty) 'reason': reason,
    },
  );
}

final inventoryRepositoryProvider = Provider(
  (ref) => InventoryRepository(ref.read(apiClientProvider)),
);

class InventoryListQuery {
  const InventoryListQuery({
    this.warehouseId = '',
    this.locationId = '',
    this.productId = '',
    this.tracking = '',
    this.sort = 'updated_at',
    this.ascending = false,
    this.page = 0,
  });
  final String warehouseId, locationId, productId, tracking, sort;
  final bool ascending;
  final int page;

  InventoryListQuery copyWith({
    String? warehouseId,
    String? locationId,
    String? productId,
    String? tracking,
    String? sort,
    bool? ascending,
    int? page,
  }) => InventoryListQuery(
    warehouseId: warehouseId ?? this.warehouseId,
    locationId: locationId ?? this.locationId,
    productId: productId ?? this.productId,
    tracking: tracking ?? this.tracking,
    sort: sort ?? this.sort,
    ascending: ascending ?? this.ascending,
    page: page ?? this.page,
  );

  @override
  bool operator ==(Object other) =>
      other is InventoryListQuery &&
      other.warehouseId == warehouseId &&
      other.locationId == locationId &&
      other.productId == productId &&
      other.tracking == tracking &&
      other.sort == sort &&
      other.ascending == ascending &&
      other.page == page;
  @override
  int get hashCode => Object.hash(
    warehouseId,
    locationId,
    productId,
    tracking,
    sort,
    ascending,
    page,
  );
}

final inventoryListProvider = FutureProvider.autoDispose
    .family<InventoryPositionPage, InventoryListQuery>(
      (ref, query) => ref
          .read(inventoryRepositoryProvider)
          .list(
            warehouseId: query.warehouseId,
            locationId: query.locationId,
            productId: query.productId,
            tracking: query.tracking,
            sort: query.sort,
            order: query.ascending ? 'asc' : 'desc',
            page: query.page + 1,
          ),
    );

final inventoryDetailProvider = FutureProvider.autoDispose
    .family<InventoryPosition, String>(
      (ref, id) => ref.read(inventoryRepositoryProvider).get(id),
    );

/// Resolves a lookup type into an id → label map so a table can show codes
/// instead of the raw UUIDs the position API returns.
///
/// A failure degrades to an empty map rather than breaking the page: a user may
/// hold inventory.read without holding product.read, and an unresolved label is
/// far better than an unusable screen.
final lookupLabelsProvider = FutureProvider.autoDispose
    .family<Map<String, String>, LookupType>((ref, type) async {
      final items = await ref.watch(lookupItemsProvider(LookupQuery(type)).future);
      return {for (final item in items) item.id: item.label};
    });

// ---------- List ----------

class InventoryListPage extends ConsumerStatefulWidget {
  const InventoryListPage({super.key});
  @override
  ConsumerState<InventoryListPage> createState() => _InventoryListPageState();
}

class _InventoryListPageState extends ConsumerState<InventoryListPage> {
  InventoryListQuery _query = const InventoryListQuery();
  LookupItem? _warehouse, _location, _product;

  void _refresh() => ref.invalidate(inventoryListProvider);

  void _update(InventoryListQuery next) => setState(() => _query = next);

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(inventoryListProvider(_query));
    final products = ref.labelsFor(LookupType.products);
    final warehouses = ref.labelsFor(LookupType.warehouses);
    final locations = ref.labelsFor(LookupType.locations);

    return PermissionGuard(
      permission: InventoryPermissions.read,
      child: state.when(
        loading: () => const MasterLoading(),
        error: (error, stack) =>
            MasterErrorState(message: '$error', onRetry: _refresh),
        data: (result) => MasterListPage<String>(
          title: 'Inventory',
          currentPage: _query.page,
          totalRecords: result.total,
          onPageChanged: (value) => _update(_query.copyWith(page: value)),
          sortField: _query.sort,
          sortDirection: _query.ascending,
          sortFields: _sortableColumns,
          onSortChanged: (field, ascending) =>
              _update(_query.copyWith(sort: field, ascending: ascending, page: 0)),
          actions: [
            MasterToolbar(
              createLabel: ref.can(InventoryPermissions.create)
                  ? 'Receive stock'
                  : null,
              onCreate: () async {
                await Navigator.push(
                  context,
                  MaterialPageRoute(
                    builder: (_) => const InventoryReceivePage(),
                  ),
                );
                _refresh();
              },
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
          filters: _InventoryFilters(
            warehouse: _warehouse,
            location: _location,
            product: _product,
            tracking: _query.tracking,
            onWarehouse: (value) {
              setState(() => _warehouse = value);
              _update(
                _query.copyWith(warehouseId: value?.id ?? '', page: 0),
              );
            },
            onLocation: (value) {
              setState(() => _location = value);
              _update(_query.copyWith(locationId: value?.id ?? '', page: 0));
            },
            onProduct: (value) {
              setState(() => _product = value);
              _update(_query.copyWith(productId: value?.id ?? '', page: 0));
            },
            onTracking: (value) =>
                _update(_query.copyWith(tracking: value ?? '', page: 0)),
            onReset: () {
              setState(() {
                _warehouse = null;
                _location = null;
                _product = null;
              });
              _update(const InventoryListQuery());
            },
            onApply: _refresh,
          ),
          columns: const [
            DataColumn(label: Text('Product')),
            DataColumn(label: Text('Warehouse')),
            DataColumn(label: Text('Location')),
            DataColumn(label: Text('Tracking')),
            DataColumn(
              label: Text('Available'),
              numeric: true,
              onSort: _positionColumnSort,
            ),
            DataColumn(
              label: Text('Reserved'),
              numeric: true,
              onSort: _positionColumnSort,
            ),
            DataColumn(label: Text('Allocated'), numeric: true),
            DataColumn(label: Text('Quarantined'), numeric: true),
            DataColumn(label: Text('On hand'), numeric: true),
            DataColumn(label: Text('Updated at'), onSort: _positionColumnSort),
            DataColumn(label: Text('Actions')),
          ],
          rows: [
            for (final item in result.items)
              DataRow(
                cells: [
                  DataCell(
                    Text(_label(products, item.productId, item.trackingSuffix)),
                  ),
                  DataCell(Text(_label(warehouses, item.warehouseId, ''))),
                  DataCell(Text(_label(locations, item.locationId, ''))),
                  DataCell(Text(item.tracking)),
                  DataCell(Text('${item.available}')),
                  DataCell(Text('${item.reserved}')),
                  DataCell(Text('${item.allocated}')),
                  DataCell(Text('${item.quarantined}')),
                  DataCell(
                    Text(
                      '${item.onHand}',
                      style: const TextStyle(fontWeight: FontWeight.w600),
                    ),
                  ),
                  DataCell(Text(_formatTimestamp(item.updatedAt))),
                  DataCell(
                    IconButton(
                      tooltip: 'View',
                      icon: const Icon(Icons.visibility_outlined),
                      onPressed: () => Navigator.push(
                        context,
                        MaterialPageRoute(
                          builder: (_) => InventoryDetailPage(id: item.id),
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

class _InventoryFilters extends StatelessWidget {
  const _InventoryFilters({
    required this.warehouse,
    required this.location,
    required this.product,
    required this.tracking,
    required this.onWarehouse,
    required this.onLocation,
    required this.onProduct,
    required this.onTracking,
    required this.onReset,
    required this.onApply,
  });
  final LookupItem? warehouse, location, product;
  final String tracking;
  final ValueChanged<LookupItem?> onWarehouse, onLocation, onProduct;
  final ValueChanged<String?> onTracking;
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
      SizedBox(
        width: 220,
        child: AppLookupDropdown(
          type: LookupType.locations,
          label: 'Location',
          value: location,
          onChanged: onLocation,
        ),
      ),
      DropdownButton<String?>(
        value: tracking.isEmpty ? null : tracking,
        hint: const Text('Tracking'),
        items: const [
          DropdownMenuItem<String?>(value: null, child: Text('All')),
          DropdownMenuItem<String?>(value: 'NONE', child: Text('None')),
          DropdownMenuItem<String?>(value: 'LOT', child: Text('Lot')),
          DropdownMenuItem<String?>(value: 'SERIAL', child: Text('Serial')),
        ],
        onChanged: onTracking,
      ),
      TextButton(onPressed: onReset, child: const Text('Reset')),
      AppButton(label: 'Apply', onPressed: onApply),
    ],
  );
}

// ---------- Detail ----------

class InventoryDetailPage extends ConsumerWidget {
  const InventoryDetailPage({super.key, required this.id});
  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) => PermissionGuard(
    permission: InventoryPermissions.read,
    child: ref
        .watch(inventoryDetailProvider(id))
        .when(
          loading: () => const MasterLoading(),
          error: (error, stack) => MasterErrorState(
            message: '$error',
            onRetry: () => ref.invalidate(inventoryDetailProvider(id)),
          ),
          data: (item) => _buildDetail(context, ref, item),
        ),
  );

  Widget _buildDetail(
    BuildContext context,
    WidgetRef ref,
    InventoryPosition item,
  ) {
    final products = ref.labelsFor(LookupType.products);
    final warehouses = ref.labelsFor(LookupType.warehouses);
    final locations = ref.labelsFor(LookupType.locations);

    Future<void> run(Future<void> Function() action, String success) async {
      try {
        await action();
        ref.invalidate(inventoryDetailProvider(id));
        ref.invalidate(inventoryListProvider);
        if (context.mounted) _toast(context, success);
      } on DioException catch (error) {
        if (context.mounted) _toast(context, apiError(error));
      }
    }

    return MasterDetailPage(
      title: _label(products, item.productId, item.trackingSuffix),
      status: _BucketSummary(position: item),
      actions: [
        for (final movement in StockMovement.values)
          PermissionGate(
            permission: movement.permission,
            child: AppButton(
              label: movement.label,
              isOutlined: true,
              onPressed: () async {
                final quantity = await showDialog<int>(
                  context: context,
                  builder: (_) => _QuantityDialog(title: movement.label),
                );
                if (quantity == null) return;
                await run(
                  () => ref
                      .read(inventoryRepositoryProvider)
                      .move(item.id, movement, quantity),
                  '${movement.label} completed',
                );
              },
            ),
          ),
        PermissionGate(
          permission: InventoryPermissions.transfer,
          child: AppButton(
            label: 'Transfer',
            isOutlined: true,
            onPressed: () async {
              final result = await showDialog<_TransferResult>(
                context: context,
                builder: (_) => const _TransferDialog(),
              );
              if (result == null) return;
              await run(
                () => ref
                    .read(inventoryRepositoryProvider)
                    .transfer(
                      item.id,
                      toWarehouseId: result.warehouseId,
                      toLocationId: result.locationId,
                      quantity: result.quantity,
                    ),
                'Stock transferred',
              );
            },
          ),
        ),
        PermissionGate(
          permission: InventoryPermissions.adjust,
          child: AppButton(
            label: 'Adjust',
            onPressed: () async {
              final result = await showDialog<_AdjustResult>(
                context: context,
                builder: (_) => _AdjustDialog(current: item.available),
              );
              if (result == null) return;
              await run(
                () => ref
                    .read(inventoryRepositoryProvider)
                    .adjust(
                      item.id,
                      quantity: result.quantity,
                      type: result.type,
                      reason: result.reason,
                    ),
                'Position adjusted',
              );
            },
          ),
        ),
      ],
      general: [
        ListTile(
          title: const Text('Warehouse'),
          subtitle: Text(_label(warehouses, item.warehouseId, '')),
        ),
        ListTile(
          title: const Text('Location'),
          subtitle: Text(_label(locations, item.locationId, '')),
        ),
        ListTile(
          title: const Text('Tracking'),
          subtitle: Text(item.tracking),
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
          title: const Text('Available'),
          subtitle: Text('${item.available}'),
        ),
        ListTile(
          title: const Text('Reserved'),
          subtitle: Text('${item.reserved}'),
        ),
        ListTile(
          title: const Text('Allocated'),
          subtitle: Text('${item.allocated}'),
        ),
        ListTile(
          title: const Text('Quarantined'),
          subtitle: Text('${item.quarantined}'),
        ),
        ListTile(
          title: const Text('On hand'),
          subtitle: Text('${item.onHand}'),
        ),
      ],
      audit: [
        ListTile(
          title: const Text('Created at'),
          subtitle: Text(_formatTimestamp(item.createdAt)),
        ),
        ListTile(
          title: const Text('Updated at'),
          subtitle: Text(_formatTimestamp(item.updatedAt)),
        ),
      ],
    );
  }
}

class _BucketSummary extends StatelessWidget {
  const _BucketSummary({required this.position});
  final InventoryPosition position;
  @override
  Widget build(BuildContext context) => Wrap(
    spacing: AppSpacing.sm,
    runSpacing: AppSpacing.xs,
    children: [
      _chip(context, 'Available', position.available),
      _chip(context, 'Reserved', position.reserved),
      _chip(context, 'Allocated', position.allocated),
      _chip(context, 'Quarantined', position.quarantined),
      _chip(context, 'On hand', position.onHand),
    ],
  );

  Widget _chip(BuildContext context, String label, int value) =>
      Chip(label: Text('$label: $value'), side: BorderSide.none);
}

// ---------- Receive ----------

class InventoryReceivePage extends ConsumerStatefulWidget {
  const InventoryReceivePage({super.key});
  @override
  ConsumerState<InventoryReceivePage> createState() =>
      _InventoryReceivePageState();
}

class _InventoryReceivePageState extends ConsumerState<InventoryReceivePage> {
  final _form = GlobalKey<FormState>();
  final _quantity = TextEditingController();
  final _lotNumber = TextEditingController();
  final _serialNumber = TextEditingController();
  LookupItem? _warehouse, _location, _product;
  String _tracking = 'NONE';
  bool _loading = false;

  @override
  void dispose() {
    _quantity.dispose();
    _lotNumber.dispose();
    _serialNumber.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (!_form.currentState!.validate()) return;
    if (_warehouse == null || _location == null || _product == null) {
      _toast(context, 'Select a warehouse, location and product');
      return;
    }
    setState(() => _loading = true);
    try {
      await ref
          .read(inventoryRepositoryProvider)
          .receive(
            ReceiveStockInput(
              warehouseId: _warehouse!.id,
              locationId: _location!.id,
              productId: _product!.id,
              tracking: _tracking,
              quantity: int.parse(_quantity.text.trim()),
              lotNumber: _tracking == 'LOT' ? _lotNumber.text.trim() : null,
              serialNumber: _tracking == 'SERIAL'
                  ? _serialNumber.text.trim()
                  : null,
            ),
          );
      ref.invalidate(inventoryListProvider);
      if (!mounted) return;
      _toast(context, 'Stock received');
      Navigator.pop(context);
    } on DioException catch (error) {
      if (mounted) _toast(context, apiError(error));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) => PermissionGuard(
    permission: InventoryPermissions.create,
    child: MasterFormPage(
      title: 'Receive stock',
      loading: _loading,
      child: Form(
        key: _form,
        child: Column(
          children: [
            AppLookupDropdown(
              type: LookupType.warehouses,
              label: 'Warehouse',
              value: _warehouse,
              onChanged: (value) => setState(() => _warehouse = value),
            ),
            AppLookupDropdown(
              type: LookupType.locations,
              label: 'Location',
              value: _location,
              onChanged: (value) => setState(() => _location = value),
            ),
            AppLookupDropdown(
              type: LookupType.products,
              label: 'Product',
              value: _product,
              onChanged: (value) => setState(() => _product = value),
            ),
            DropdownButtonFormField<String>(
              initialValue: _tracking,
              decoration: const InputDecoration(labelText: 'Tracking'),
              items: const [
                DropdownMenuItem(value: 'NONE', child: Text('None')),
                DropdownMenuItem(value: 'LOT', child: Text('Lot')),
                DropdownMenuItem(value: 'SERIAL', child: Text('Serial')),
              ],
              onChanged: (value) => setState(() => _tracking = value ?? 'NONE'),
            ),
            if (_tracking == 'LOT')
              TextFormField(
                controller: _lotNumber,
                decoration: const InputDecoration(labelText: 'Lot number'),
                validator: (value) => (value ?? '').trim().isEmpty
                    ? 'Lot tracking requires a lot number'
                    : null,
              ),
            if (_tracking == 'SERIAL')
              TextFormField(
                controller: _serialNumber,
                decoration: const InputDecoration(labelText: 'Serial number'),
                validator: (value) => (value ?? '').trim().isEmpty
                    ? 'Serial tracking requires a serial number'
                    : null,
              ),
            TextFormField(
              controller: _quantity,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(labelText: 'Quantity'),
              validator: _positiveQuantity,
            ),
            const SizedBox(height: AppSpacing.md),
            AppButton(label: 'Receive', loading: _loading, onPressed: _save),
          ],
        ),
      ),
    ),
  );
}

// ---------- Dialogs ----------

class _QuantityDialog extends StatefulWidget {
  const _QuantityDialog({required this.title});
  final String title;
  @override
  State<_QuantityDialog> createState() => _QuantityDialogState();
}

class _QuantityDialogState extends State<_QuantityDialog> {
  final _form = GlobalKey<FormState>();
  final _quantity = TextEditingController();
  @override
  void dispose() {
    _quantity.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.title),
    content: Form(
      key: _form,
      child: TextFormField(
        controller: _quantity,
        autofocus: true,
        keyboardType: TextInputType.number,
        decoration: const InputDecoration(labelText: 'Quantity'),
        validator: _positiveQuantity,
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      FilledButton(
        onPressed: () {
          if (!_form.currentState!.validate()) return;
          Navigator.pop(context, int.parse(_quantity.text.trim()));
        },
        child: const Text('Confirm'),
      ),
    ],
  );
}

class _TransferResult {
  const _TransferResult(this.warehouseId, this.locationId, this.quantity);
  final String warehouseId, locationId;
  final int quantity;
}

class _TransferDialog extends StatefulWidget {
  const _TransferDialog();
  @override
  State<_TransferDialog> createState() => _TransferDialogState();
}

class _TransferDialogState extends State<_TransferDialog> {
  final _form = GlobalKey<FormState>();
  final _quantity = TextEditingController();
  LookupItem? _warehouse, _location;
  @override
  void dispose() {
    _quantity.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Transfer stock'),
    content: SizedBox(
      width: 360,
      child: Form(
        key: _form,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            AppLookupDropdown(
              type: LookupType.warehouses,
              label: 'Destination warehouse',
              value: _warehouse,
              onChanged: (value) => setState(() => _warehouse = value),
            ),
            AppLookupDropdown(
              type: LookupType.locations,
              label: 'Destination location',
              value: _location,
              onChanged: (value) => setState(() => _location = value),
            ),
            TextFormField(
              controller: _quantity,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(labelText: 'Quantity'),
              validator: _positiveQuantity,
            ),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      FilledButton(
        onPressed: () {
          if (!_form.currentState!.validate()) return;
          if (_warehouse == null || _location == null) {
            _toast(context, 'Select a destination warehouse and location');
            return;
          }
          Navigator.pop(
            context,
            _TransferResult(
              _warehouse!.id,
              _location!.id,
              int.parse(_quantity.text.trim()),
            ),
          );
        },
        child: const Text('Transfer'),
      ),
    ],
  );
}

class _AdjustResult {
  const _AdjustResult(this.quantity, this.type, this.reason);
  final int quantity;
  final AdjustmentType type;
  final String reason;
}

class _AdjustDialog extends StatefulWidget {
  const _AdjustDialog({required this.current});
  final int current;
  @override
  State<_AdjustDialog> createState() => _AdjustDialogState();
}

class _AdjustDialogState extends State<_AdjustDialog> {
  final _form = GlobalKey<FormState>();
  late final _quantity = TextEditingController(text: '${widget.current}');
  final _reason = TextEditingController();
  AdjustmentType _type = AdjustmentType.cycleCount;
  @override
  void dispose() {
    _quantity.dispose();
    _reason.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Adjust position'),
    content: SizedBox(
      width: 360,
      child: Form(
        key: _form,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextFormField(
              controller: _quantity,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(
                labelText: 'Counted available quantity',
              ),
              validator: (value) {
                final parsed = int.tryParse((value ?? '').trim());
                if (parsed == null) return 'Enter a whole number';
                if (parsed < 0) return 'Quantity cannot be negative';
                return null;
              },
            ),
            DropdownButtonFormField<AdjustmentType>(
              initialValue: _type,
              decoration: const InputDecoration(labelText: 'Reason type'),
              items: [
                for (final type in AdjustmentType.values)
                  DropdownMenuItem(value: type, child: Text(type.label)),
              ],
              onChanged: (value) =>
                  setState(() => _type = value ?? AdjustmentType.cycleCount),
            ),
            TextFormField(
              controller: _reason,
              maxLines: 2,
              decoration: const InputDecoration(labelText: 'Reason'),
            ),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      FilledButton(
        onPressed: () {
          if (!_form.currentState!.validate()) return;
          Navigator.pop(
            context,
            _AdjustResult(
              int.parse(_quantity.text.trim()),
              _type,
              _reason.text.trim(),
            ),
          );
        },
        child: const Text('Adjust'),
      ),
    ],
  );
}

// ---------- Shared helpers ----------

extension on InventoryPosition {
  /// The lot or serial that individuates this position, rendered for a label.
  String get trackingSuffix {
    if (lotNumber.isNotEmpty) return ' · $lotNumber';
    if (serialNumber.isNotEmpty) return ' · $serialNumber';
    return '';
  }
}

extension InventoryLookupRef on WidgetRef {
  /// Id → label map for a lookup type, degrading to empty when the caller is not
  /// permitted to read that catalogue.
  Map<String, String> labelsFor(LookupType type) =>
      watch(lookupLabelsProvider(type)).value ?? const {};
}

String _label(Map<String, String> labels, String id, String suffix) =>
    '${labels[id] ?? _shortId(id)}$suffix';

String _shortId(String id) => id.length > 8 ? '${id.substring(0, 8)}…' : id;

String _formatTimestamp(String value) => value.length >= 16
    ? value.substring(0, 16).replaceFirst('T', ' ')
    : value;

String? _positiveQuantity(String? value) {
  final parsed = int.tryParse((value ?? '').trim());
  if (parsed == null) return 'Enter a whole number';
  if (parsed <= 0) return 'Quantity must be greater than zero';
  return null;
}

void _toast(BuildContext context, String message) =>
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
