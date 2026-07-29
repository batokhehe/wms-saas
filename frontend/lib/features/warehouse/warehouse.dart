import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/network/api_client.dart' as network;
import '../../shared/widgets/buttons/app_button.dart';
import '../master/presentation/pages/master_detail_page.dart';
import '../master/presentation/pages/master_form_page.dart';
import '../master/presentation/pages/master_list_page.dart';
import '../master/presentation/widgets/lifecycle_dialog.dart';
import '../master/presentation/widgets/master_filter_bar.dart';
import '../master/presentation/widgets/master_states.dart';
import '../master/presentation/widgets/master_toolbar.dart';
import '../master/presentation/widgets/status_chip.dart';
import '../../shared/widgets/status/status_badge.dart';

class Warehouse {
  const Warehouse({
    required this.id,
    required this.code,
    required this.name,
    required this.status,
    required this.type,
    required this.createdAt,
    this.description = '',
  });
  final String id, code, name, status, type, createdAt, description;
  factory Warehouse.fromJson(Map<String, dynamic> json) => Warehouse(
    id: json['id'],
    code: json['code'],
    name: json['name'],
    status: json['status'],
    type: json['type'],
    createdAt: json['created_at'] ?? '',
    description: json['description'] ?? '',
  );
}

class WarehouseRepository {
  WarehouseRepository(this._api);
  final network.ApiClient _api;
  Future<List<Warehouse>> list({String search = ''}) async {
    final response = await _api.dio.get(
      '/warehouses',
      queryParameters: search.isEmpty ? null : {'search': search},
    );
    return (response.data['data'] as List)
        .map((item) => Warehouse.fromJson(item))
        .toList();
  }

  Future<Warehouse> get(String id) async =>
      Warehouse.fromJson((await _api.dio.get('/warehouses/$id')).data['data']);
  Future<void> save({
    Warehouse? value,
    required String code,
    required String name,
    required String description,
    required String type,
  }) => value == null
      ? _api.dio.post(
          '/warehouses',
          data: {
            'code': code,
            'name': name,
            'description': description,
            'type': type,
          },
        )
      : _api.dio.put(
          '/warehouses/${value.id}',
          data: {'name': name, 'description': description},
        );
  Future<void> lifecycle(Warehouse value, bool active) => _api.dio.patch(
    '/warehouses/${value.id}/${active ? 'activate' : 'deactivate'}',
  );
}

final warehouseRepositoryProvider = Provider(
  (ref) => WarehouseRepository(ref.read(network.apiClientProvider)),
);
final warehouseListProvider = FutureProvider.autoDispose
    .family<List<Warehouse>, String>(
      (ref, search) =>
          ref.read(warehouseRepositoryProvider).list(search: search),
    );

class WarehouseListPage extends ConsumerStatefulWidget {
  const WarehouseListPage({super.key});
  @override
  ConsumerState<WarehouseListPage> createState() => _WarehouseListPageState();
}

class _WarehouseListPageState extends ConsumerState<WarehouseListPage> {
  String _search = '';
  @override
  Widget build(BuildContext context) {
    final state = ref.watch(warehouseListProvider(_search));
    return state.when(
      loading: () => const MasterLoading(),
      error: (error, stack) => MasterErrorState(
        message: '$error',
        onRetry: () => ref.invalidate(warehouseListProvider),
      ),
      data: (items) => MasterListPage(
        title: 'Warehouses',
        actions: [
          MasterToolbar(
            createLabel: 'Create warehouse',
            onCreate: () => Navigator.push(
              context,
              MaterialPageRoute(builder: (_) => const WarehouseFormPage()),
            ),
          ),
        ],
        filters: MasterFilterBar(
          onSearch: (value) => setState(() => _search = value),
          onRefresh: () => ref.invalidate(warehouseListProvider),
          onClear: () => setState(() => _search = ''),
        ),
        columns: const [
          DataColumn(label: Text('Code')),
          DataColumn(label: Text('Name')),
          DataColumn(label: Text('Status')),
          DataColumn(label: Text('Created at')),
          DataColumn(label: Text('Actions')),
        ],
        rows: [
          for (final item in items)
            DataRow(
              cells: [
                DataCell(Text(item.code)),
                DataCell(Text(item.name)),
                DataCell(
                  StatusChip(
                    status: item.status == 'ACTIVE'
                        ? AppStatus.active
                        : AppStatus.inactive,
                  ),
                ),
                DataCell(Text(item.createdAt)),
                DataCell(
                  IconButton(
                    tooltip: 'View',
                    icon: const Icon(Icons.visibility_outlined),
                    onPressed: () => Navigator.push(
                      context,
                      MaterialPageRoute(
                        builder: (_) => WarehouseDetailPage(id: item.id),
                      ),
                    ),
                  ),
                ),
              ],
            ),
        ],
      ),
    );
  }
}

class WarehouseDetailPage extends ConsumerWidget {
  const WarehouseDetailPage({super.key, required this.id});
  final String id;
  @override
  Widget build(BuildContext context, WidgetRef ref) => FutureBuilder<Warehouse>(
    future: ref.read(warehouseRepositoryProvider).get(id),
    builder: (context, state) {
      if (state.hasError) return MasterErrorState(message: '${state.error}');
      if (!state.hasData) return const MasterLoading();
      final item = state.data!;
      final active = item.status == 'ACTIVE';
      return MasterDetailPage(
        title: item.name,
        status: StatusChip(
          status: active ? AppStatus.active : AppStatus.inactive,
        ),
        actions: [
          AppButton(
            label: 'Edit',
            onPressed: () => Navigator.push(
              context,
              MaterialPageRoute(builder: (_) => WarehouseFormPage(value: item)),
            ),
          ),
          AppButton(
            label: active ? 'Deactivate' : 'Activate',
            isOutlined: true,
            onPressed: () => showDialog(
              context: context,
              builder: (_) => LifecycleDialog(
                itemName: item.name,
                activate: !active,
                onConfirm: () async {
                  await ref
                      .read(warehouseRepositoryProvider)
                      .lifecycle(item, !active);
                  if (context.mounted) Navigator.pop(context);
                },
              ),
            ),
          ),
        ],
        general: [
          ListTile(title: const Text('Code'), subtitle: Text(item.code)),
          ListTile(title: const Text('Type'), subtitle: Text(item.type)),
          ListTile(
            title: const Text('Description'),
            subtitle: Text(item.description),
          ),
        ],
        audit: [
          ListTile(
            title: const Text('Created at'),
            subtitle: Text(item.createdAt),
          ),
        ],
      );
    },
  );
}

class WarehouseFormPage extends ConsumerStatefulWidget {
  const WarehouseFormPage({super.key, this.value});
  final Warehouse? value;
  @override
  ConsumerState<WarehouseFormPage> createState() => _WarehouseFormPageState();
}

class _WarehouseFormPageState extends ConsumerState<WarehouseFormPage> {
  late final code = TextEditingController(text: widget.value?.code);
  late final name = TextEditingController(text: widget.value?.name);
  late final description = TextEditingController(
    text: widget.value?.description,
  );
  String type = 'MAIN';
  bool loading = false;
  @override
  Widget build(BuildContext context) => MasterFormPage(
    title: widget.value == null ? 'Create warehouse' : 'Edit warehouse',
    loading: loading,
    child: Column(
      children: [
        TextField(
          controller: code,
          enabled: widget.value == null,
          decoration: const InputDecoration(labelText: 'Warehouse code'),
        ),
        TextField(
          controller: name,
          decoration: const InputDecoration(labelText: 'Warehouse name'),
        ),
        TextField(
          controller: description,
          maxLines: 3,
          decoration: const InputDecoration(labelText: 'Description'),
        ),
        DropdownButtonFormField(
          initialValue: type,
          items: const [DropdownMenuItem(value: 'MAIN', child: Text('Main'))],
          onChanged: (value) => setState(() => type = value!),
          decoration: const InputDecoration(labelText: 'Type'),
        ),
        AppButton(
          label: 'Save',
          loading: loading,
          onPressed: () async {
            if (code.text.isEmpty || name.text.isEmpty) return;
            setState(() => loading = true);
            try {
              await ref
                  .read(warehouseRepositoryProvider)
                  .save(
                    value: widget.value,
                    code: code.text,
                    name: name.text,
                    description: description.text,
                    type: type,
                  );
              if (context.mounted) Navigator.pop(context);
            } finally {
              if (mounted) setState(() => loading = false);
            }
          },
        ),
      ],
    ),
  );
}
