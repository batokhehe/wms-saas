import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../shared/widgets/buttons/app_button.dart';
import '../auth/presentation/controllers/auth_controller.dart';
import '../lookups/domain/entities/lookup_item.dart';
import '../lookups/domain/repositories/lookup_repository.dart';
import '../lookups/presentation/widgets/app_lookup_dropdown.dart';
import '../master/presentation/pages/master_detail_page.dart';
import '../master/presentation/pages/master_form_page.dart';
import '../master/presentation/pages/master_list_page.dart';
import '../master/presentation/widgets/master_filter_bar.dart';
import '../master/presentation/widgets/master_states.dart';
import '../master/presentation/widgets/master_toolbar.dart';
import '../master/presentation/widgets/lifecycle_dialog.dart';
import '../master/presentation/widgets/status_chip.dart';
import '../../shared/widgets/status/status_badge.dart';

void _locationColumnSort(int columnIndex, bool ascending) {}

class LocationItem {
  const LocationItem({
    required this.id,
    required this.code,
    required this.warehouseId,
    required this.zone,
    required this.status,
    required this.createdAt,
  });
  final String id, code, warehouseId, zone, status, createdAt;
  factory LocationItem.fromJson(Map<String, dynamic> j) {
    final c = j['coordinate'] as Map<String, dynamic>;
    return LocationItem(
      id: j['id'],
      code: j['code'],
      warehouseId: j['warehouse_id'],
      zone: c['zone'],
      status: j['status'],
      createdAt: j['created_at'] ?? '',
    );
  }
}

class LocationPage {
  const LocationPage(this.items, this.total);
  final List<LocationItem> items;
  final int total;
}

class LocationRepository {
  LocationRepository(this.api);
  final ApiClient api;
  Future<LocationPage> list({
    String search = '',
    String status = '',
    String sort = 'priority',
    String order = 'asc',
    int page = 1,
    int limit = 10,
  }) async {
    final r = await api.dio.get(
      '/locations',
      queryParameters: {
        'page': page,
        'limit': limit,
        'sort': sort,
        'order': order,
        if (search.isNotEmpty) 'search': search,
        if (status.isNotEmpty) 'status': status,
      },
    );
    final meta = r.data['meta']['pagination'] as Map<String, dynamic>;
    return LocationPage(
      (r.data['data'] as List).map((v) => LocationItem.fromJson(v)).toList(),
      meta['total'] as int,
    );
  }

  Future<LocationItem> get(String id) async =>
      LocationItem.fromJson((await api.dio.get('/locations/$id')).data['data']);
  Future<void> create(String warehouse, String code, String zone) =>
      api.dio.post(
        '/locations',
        data: {'warehouse_id': warehouse, 'code': code, 'zone': zone},
      );
  Future<void> update(
    LocationItem x, {
    int? pickingPriority,
    bool? allowMixedSku,
    bool? allowOverflow,
  }) => api.dio.put(
    '/locations/${x.id}',
    data: {
      'picking_priority': ?pickingPriority,
      'allow_mixed_sku': ?allowMixedSku,
      'allow_overflow': ?allowOverflow,
    },
  );
  Future<void> lifecycle(LocationItem x, bool active) =>
      api.dio.patch('/locations/${x.id}/${active ? 'activate' : 'deactivate'}');
}

final locationRepoProvider = Provider(
  (ref) => LocationRepository(ref.read(apiClientProvider)),
);
final locationListProvider = FutureProvider.autoDispose
    .family<LocationPage, LocationListQuery>(
      (ref, q) => ref
          .read(locationRepoProvider)
          .list(
            search: q.search,
            status: q.status,
            sort: q.sort,
            order: q.ascending ? 'asc' : 'desc',
            page: q.page + 1,
          ),
    );

class LocationListQuery {
  const LocationListQuery(
    this.search,
    this.page, {
    this.status = '',
    this.sort = 'priority',
    this.ascending = true,
  });
  final String search, status, sort;
  final int page;
  final bool ascending;
  @override
  bool operator ==(Object other) =>
      other is LocationListQuery &&
      other.search == search &&
      other.page == page &&
      other.status == status &&
      other.sort == sort &&
      other.ascending == ascending;
  @override
  int get hashCode => Object.hash(search, page, status, sort, ascending);
}

class LocationListPage extends ConsumerStatefulWidget {
  const LocationListPage({super.key});
  @override
  ConsumerState<LocationListPage> createState() => _LocationListPageState();
}

class _LocationListPageState extends ConsumerState<LocationListPage> {
  String s = '', status = '', sort = 'priority';
  bool ascending = true;
  int page = 0;
  @override
  Widget build(BuildContext c) {
    final x = ref.watch(
      locationListProvider(
        LocationListQuery(
          s,
          page,
          status: status,
          sort: sort,
          ascending: ascending,
        ),
      ),
    );
    return x.when(
      loading: () => const MasterLoading(),
      error: (e, _) => MasterErrorState(
        message: '$e',
        onRetry: () => ref.invalidate(locationListProvider),
      ),
      data: (result) => MasterListPage<String>(
        title: 'Locations',
        currentPage: page,
        totalRecords: result.total,
        onPageChanged: (v) => setState(() => page = v),
        sortField: sort,
        sortDirection: ascending,
        sortFields: const ['code', 'zone', 'status', ''],
        onSortChanged: (field, asc) => setState(() {
          sort = field;
          ascending = asc;
          page = 0;
        }),
        statusFilter: status.isEmpty ? null : status,
        availableStatuses: const [
          MasterStatusOption(label: 'Active', value: 'ACTIVE'),
          MasterStatusOption(label: 'Inactive', value: 'INACTIVE'),
          MasterStatusOption(label: 'Locked', value: 'LOCKED'),
          MasterStatusOption(label: 'Maintenance', value: 'MAINTENANCE'),
        ],
        onStatusChanged: (value) => setState(() {
          status = value ?? '';
          page = 0;
        }),
        actions: [
          MasterToolbar(
            createLabel: 'Create location',
            onCreate: () => Navigator.push(
              c,
              MaterialPageRoute(builder: (_) => const LocationFormPage()),
            ),
          ),
        ],
        filters: MasterFilterBar<String>(
          onSearch: (v) => setState(() {
            s = v;
            page = 0;
          }),
          statusFilter: status.isEmpty ? null : status,
          availableStatuses: const [
            MasterStatusOption(label: 'Active', value: 'ACTIVE'),
            MasterStatusOption(label: 'Inactive', value: 'INACTIVE'),
            MasterStatusOption(label: 'Locked', value: 'LOCKED'),
            MasterStatusOption(label: 'Maintenance', value: 'MAINTENANCE'),
          ],
          onStatusChanged: (v) => setState(() {
            status = v ?? '';
            page = 0;
          }),
        ),
        columns: const [
          DataColumn(label: Text('Code'), onSort: _locationColumnSort),
          DataColumn(label: Text('Zone'), onSort: _locationColumnSort),
          DataColumn(label: Text('Status'), onSort: _locationColumnSort),
          DataColumn(label: Text('Actions')),
        ],
        rows: [
          for (final i in result.items)
            DataRow(
              cells: [
                DataCell(Text(i.code)),
                DataCell(Text(i.zone)),
                DataCell(
                  StatusChip(
                    status: i.status == 'ACTIVE'
                        ? AppStatus.active
                        : AppStatus.inactive,
                  ),
                ),
                DataCell(
                  IconButton(
                    onPressed: () => Navigator.push(
                      c,
                      MaterialPageRoute(
                        builder: (_) => LocationDetailPage(id: i.id),
                      ),
                    ),
                    icon: const Icon(Icons.visibility_outlined),
                  ),
                ),
              ],
            ),
        ],
      ),
    );
  }
}

class LocationDetailPage extends ConsumerWidget {
  const LocationDetailPage({super.key, required this.id});
  final String id;
  @override
  Widget build(BuildContext c, WidgetRef r) => FutureBuilder(
    future: r.read(locationRepoProvider).get(id),
    builder: (c, s) {
      if (!s.hasData) return const MasterLoading();
      final x = s.data!;
      final active = x.status == 'ACTIVE';
      return MasterDetailPage(
        title: x.code,
        status: StatusChip(
          status: active ? AppStatus.active : AppStatus.inactive,
        ),
        actions: [
          AppButton(
            label: 'Edit',
            onPressed: () => Navigator.push(
              c,
              MaterialPageRoute(builder: (_) => LocationEditPage(value: x)),
            ),
          ),
          AppButton(
            label: active ? 'Deactivate' : 'Activate',
            isOutlined: true,
            onPressed: () => showDialog(
              context: c,
              builder: (_) => LifecycleDialog(
                itemName: x.code,
                activate: !active,
                onConfirm: () async {
                  await r.read(locationRepoProvider).lifecycle(x, !active);
                  if (c.mounted) {
                    Navigator.pop(c);
                    ScaffoldMessenger.of(c).showSnackBar(
                      const SnackBar(content: Text('Location status updated')),
                    );
                  }
                },
              ),
            ),
          ),
        ],
        general: [
          ListTile(title: const Text('Zone'), subtitle: Text(x.zone)),
          ListTile(
            title: const Text('Warehouse'),
            subtitle: Text(x.warehouseId),
          ),
        ],
        audit: [
          ListTile(
            title: const Text('Created at'),
            subtitle: Text(x.createdAt),
          ),
        ],
      );
    },
  );
}

class LocationEditPage extends ConsumerStatefulWidget {
  const LocationEditPage({super.key, required this.value});
  final LocationItem value;
  @override
  ConsumerState<LocationEditPage> createState() => _LocationEditPageState();
}

class _LocationEditPageState extends ConsumerState<LocationEditPage> {
  final priority = TextEditingController();
  bool loading = false;
  @override
  Widget build(BuildContext c) => MasterFormPage(
    title: 'Edit location',
    loading: loading,
    child: Column(
      children: [
        TextField(
          controller: priority,
          keyboardType: TextInputType.number,
          decoration: const InputDecoration(labelText: 'Picking priority'),
        ),
        AppButton(
          label: 'Save',
          loading: loading,
          onPressed: () async {
            setState(() => loading = true);
            try {
              await ref
                  .read(locationRepoProvider)
                  .update(
                    widget.value,
                    pickingPriority: int.tryParse(priority.text),
                  );
              if (c.mounted) {
                Navigator.pop(c);
                ScaffoldMessenger.of(c).showSnackBar(
                  const SnackBar(content: Text('Location updated')),
                );
              }
            } catch (_) {
              if (c.mounted) {
                ScaffoldMessenger.of(c).showSnackBar(
                  const SnackBar(content: Text('Unable to update location')),
                );
              }
            } finally {
              if (mounted) setState(() => loading = false);
            }
          },
        ),
      ],
    ),
  );
}

class LocationFormPage extends ConsumerStatefulWidget {
  const LocationFormPage({super.key});
  @override
  ConsumerState<LocationFormPage> createState() => _LocationFormPageState();
}

class _LocationFormPageState extends ConsumerState<LocationFormPage> {
  LookupItem? warehouse;
  final code = TextEditingController();
  final zone = TextEditingController();
  @override
  Widget build(BuildContext c) => MasterFormPage(
    title: 'Create location',
    child: Column(
      children: [
        AppLookupDropdown(
          type: LookupType.warehouses,
          label: 'Warehouse',
          value: warehouse,
          onChanged: (v) => setState(() => warehouse = v),
        ),
        TextField(
          controller: code,
          decoration: const InputDecoration(labelText: 'Code'),
        ),
        TextField(
          controller: zone,
          decoration: const InputDecoration(labelText: 'Zone'),
        ),
        AppButton(
          label: 'Save',
          onPressed: () => ref
              .read(locationRepoProvider)
              .create(warehouse!.id, code.text, zone.text),
        ),
      ],
    ),
  );
}
