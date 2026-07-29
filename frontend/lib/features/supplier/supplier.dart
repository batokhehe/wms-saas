import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../shared/widgets/buttons/app_button.dart';
import '../../shared/widgets/status/status_badge.dart';
import '../auth/presentation/controllers/auth_controller.dart';
import '../auth/presentation/controllers/permission_controller.dart';
import '../master/presentation/pages/master_detail_page.dart';
import '../master/presentation/pages/master_form_page.dart';
import '../master/presentation/pages/master_list_page.dart';
import '../master/presentation/widgets/lifecycle_dialog.dart';
import '../master/presentation/widgets/master_filter_bar.dart';
import '../master/presentation/widgets/master_states.dart';
import '../master/presentation/widgets/master_toolbar.dart';
import '../master/presentation/widgets/status_chip.dart';

/// Permission codes this module's controls are gated on. They mirror the codes
/// the supplier routes enforce server-side.
abstract final class SupplierPermissions {
  static const read = 'supplier.read';
  static const create = 'supplier.create';
  static const update = 'supplier.update';
  static const activate = 'supplier.activate';
}

/// Sort keys the backend allows. Anything else is rejected by the API's
/// allow-list, so the table only offers these.
const _sortableColumns = ['code', 'name', '', '', 'status', 'created_at', ''];

void _supplierColumnSort(int columnIndex, bool ascending) {}

// ---------- Model ----------

class Supplier {
  const Supplier({
    required this.id,
    required this.code,
    required this.name,
    required this.status,
    required this.createdAt,
    this.email = '',
    this.phone = '',
    this.taxNumber = '',
    this.address = '',
    this.city = '',
    this.province = '',
    this.country = '',
    this.postalCode = '',
    this.updatedAt = '',
  });
  final String id, code, name, status, createdAt;
  final String email, phone, taxNumber;
  final String address, city, province, country, postalCode;
  final String updatedAt;

  bool get isActive => status == 'ACTIVE';

  factory Supplier.fromJson(Map<String, dynamic> json) => Supplier(
    id: json['id'] as String,
    code: json['code'] as String,
    name: json['name'] as String,
    status: json['status'] as String,
    createdAt: json['created_at'] as String? ?? '',
    updatedAt: json['updated_at'] as String? ?? '',
    email: json['email'] as String? ?? '',
    phone: json['phone'] as String? ?? '',
    taxNumber: json['tax_number'] as String? ?? '',
    address: json['address'] as String? ?? '',
    city: json['city'] as String? ?? '',
    province: json['province'] as String? ?? '',
    country: json['country'] as String? ?? '',
    postalCode: json['postal_code'] as String? ?? '',
  );
}

/// One server page of suppliers plus the server-reported total.
class SupplierPage {
  const SupplierPage(this.items, this.total);
  final List<Supplier> items;
  final int total;
}

/// The mutable attributes of a supplier. `PUT /suppliers/:id` replaces the whole
/// editable set, so create and update send the same payload minus the code.
class SupplierInput {
  const SupplierInput({
    required this.code,
    required this.name,
    this.email = '',
    this.phone = '',
    this.taxNumber = '',
    this.address = '',
    this.city = '',
    this.province = '',
    this.country = '',
    this.postalCode = '',
  });
  final String code, name, email, phone, taxNumber;
  final String address, city, province, country, postalCode;

  Map<String, dynamic> toJson({required bool withCode}) => {
    if (withCode) 'code': code,
    'name': name,
    'email': email,
    'phone': phone,
    'tax_number': taxNumber,
    'address': address,
    'city': city,
    'province': province,
    'country': country,
    'postal_code': postalCode,
  };
}

// ---------- Data ----------

class SupplierRepository {
  SupplierRepository(this._api);
  final ApiClient _api;

  Future<SupplierPage> list({
    String search = '',
    String status = '',
    String sort = 'code',
    String order = 'asc',
    int page = 1,
    int limit = 10,
  }) async {
    final response = await _api.dio.get(
      '/suppliers',
      queryParameters: {
        'page': page,
        'limit': limit,
        'sort': sort,
        'order': order,
        if (search.isNotEmpty) 'search': search,
        if (status.isNotEmpty) 'status': status,
      },
    );
    final meta = response.data['meta']['pagination'] as Map<String, dynamic>;
    return SupplierPage(
      (response.data['data'] as List)
          .map((item) => Supplier.fromJson(item as Map<String, dynamic>))
          .toList(),
      meta['total'] as int,
    );
  }

  Future<Supplier> get(String id) async => Supplier.fromJson(
    (await _api.dio.get('/suppliers/$id')).data['data'] as Map<String, dynamic>,
  );

  Future<void> create(SupplierInput input) =>
      _api.dio.post('/suppliers', data: input.toJson(withCode: true));

  Future<void> update(String id, SupplierInput input) =>
      _api.dio.put('/suppliers/$id', data: input.toJson(withCode: false));

  /// Suppliers are never deleted — the backend exposes no DELETE. A supplier is
  /// retired by deactivation, which stops every new purchase order to them.
  Future<void> lifecycle(String id, bool activate) =>
      _api.dio.patch('/suppliers/$id/${activate ? 'activate' : 'deactivate'}');
}

final supplierRepositoryProvider = Provider(
  (ref) => SupplierRepository(ref.read(apiClientProvider)),
);

class SupplierListQuery {
  const SupplierListQuery(
    this.search,
    this.page, {
    this.status = '',
    this.sort = 'code',
    this.ascending = true,
  });
  final String search, status, sort;
  final int page;
  final bool ascending;
  @override
  bool operator ==(Object other) =>
      other is SupplierListQuery &&
      other.search == search &&
      other.page == page &&
      other.status == status &&
      other.sort == sort &&
      other.ascending == ascending;
  @override
  int get hashCode => Object.hash(search, page, status, sort, ascending);
}

final supplierListProvider = FutureProvider.autoDispose
    .family<SupplierPage, SupplierListQuery>(
      (ref, query) => ref
          .read(supplierRepositoryProvider)
          .list(
            search: query.search,
            status: query.status,
            sort: query.sort,
            order: query.ascending ? 'asc' : 'desc',
            page: query.page + 1,
          ),
    );

final supplierDetailProvider = FutureProvider.autoDispose
    .family<Supplier, String>(
      (ref, id) => ref.read(supplierRepositoryProvider).get(id),
    );

// ---------- List ----------

class SupplierListPage extends ConsumerStatefulWidget {
  const SupplierListPage({super.key});
  @override
  ConsumerState<SupplierListPage> createState() => _SupplierListPageState();
}

class _SupplierListPageState extends ConsumerState<SupplierListPage> {
  String _search = '', _status = '', _sort = 'code';
  bool _ascending = true;
  int _page = 0;

  static const _statuses = [
    MasterStatusOption(label: 'Active', value: 'ACTIVE'),
    MasterStatusOption(label: 'Inactive', value: 'INACTIVE'),
  ];

  void _refresh() => ref.invalidate(supplierListProvider);

  @override
  Widget build(BuildContext context) {
    final query = SupplierListQuery(
      _search,
      _page,
      status: _status,
      sort: _sort,
      ascending: _ascending,
    );
    final state = ref.watch(supplierListProvider(query));
    return PermissionGuard(
      permission: SupplierPermissions.read,
      child: state.when(
        loading: () => const MasterLoading(),
        error: (error, stack) =>
            MasterErrorState(message: '$error', onRetry: _refresh),
        data: (result) => MasterListPage<String>(
          title: 'Suppliers',
          currentPage: _page,
          totalRecords: result.total,
          onPageChanged: (value) => setState(() => _page = value),
          sortField: _sort,
          sortDirection: _ascending,
          sortFields: _sortableColumns,
          onSortChanged: (field, ascending) => setState(() {
            _sort = field;
            _ascending = ascending;
            _page = 0;
          }),
          actions: [
            MasterToolbar(
              createLabel: ref.can(SupplierPermissions.create)
                  ? 'Create supplier'
                  : null,
              onCreate: () => Navigator.push(
                context,
                MaterialPageRoute(builder: (_) => const SupplierFormPage()),
              ),
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
          filters: MasterFilterBar<String>(
            onSearch: (value) => setState(() {
              _search = value;
              _page = 0;
            }),
            statusFilter: _status.isEmpty ? null : _status,
            availableStatuses: _statuses,
            onStatusChanged: (value) => setState(() {
              _status = value ?? '';
              _page = 0;
            }),
            onRefresh: _refresh,
            onClear: () => setState(() {
              _search = '';
              _status = '';
              _page = 0;
            }),
          ),
          columns: const [
            DataColumn(label: Text('Code'), onSort: _supplierColumnSort),
            DataColumn(label: Text('Name'), onSort: _supplierColumnSort),
            DataColumn(label: Text('City')),
            DataColumn(label: Text('Phone')),
            DataColumn(label: Text('Status'), onSort: _supplierColumnSort),
            DataColumn(label: Text('Created at'), onSort: _supplierColumnSort),
            DataColumn(label: Text('Actions')),
          ],
          rows: [
            for (final item in result.items)
              DataRow(
                cells: [
                  DataCell(Text(item.code)),
                  DataCell(Text(item.name)),
                  DataCell(Text(item.city)),
                  DataCell(Text(item.phone)),
                  DataCell(
                    StatusChip(
                      status: item.isActive
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
                          builder: (_) => SupplierDetailPage(id: item.id),
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

// ---------- Detail ----------

class SupplierDetailPage extends ConsumerWidget {
  const SupplierDetailPage({super.key, required this.id});
  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) => PermissionGuard(
    permission: SupplierPermissions.read,
    child: ref
        .watch(supplierDetailProvider(id))
        .when(
          loading: () => const MasterLoading(),
          error: (error, stack) => MasterErrorState(
            message: '$error',
            onRetry: () => ref.invalidate(supplierDetailProvider(id)),
          ),
          data: (item) => MasterDetailPage(
            title: item.name,
            status: StatusChip(
              status: item.isActive ? AppStatus.active : AppStatus.inactive,
            ),
            actions: [
              PermissionGate(
                permission: SupplierPermissions.update,
                child: AppButton(
                  label: 'Edit',
                  onPressed: () async {
                    await Navigator.push(
                      context,
                      MaterialPageRoute(
                        builder: (_) => SupplierFormPage(value: item),
                      ),
                    );
                    ref.invalidate(supplierDetailProvider(id));
                    ref.invalidate(supplierListProvider);
                  },
                ),
              ),
              PermissionGate(
                permission: SupplierPermissions.activate,
                child: AppButton(
                  label: item.isActive ? 'Deactivate' : 'Activate',
                  isOutlined: true,
                  onPressed: () => showDialog<void>(
                    context: context,
                    builder: (dialogContext) => LifecycleDialog(
                      itemName: item.name,
                      activate: !item.isActive,
                      onConfirm: () async {
                        Navigator.pop(dialogContext);
                        try {
                          await ref
                              .read(supplierRepositoryProvider)
                              .lifecycle(item.id, !item.isActive);
                          ref.invalidate(supplierDetailProvider(id));
                          ref.invalidate(supplierListProvider);
                          if (context.mounted) {
                            _toast(context, 'Supplier status updated');
                          }
                        } on DioException catch (error) {
                          if (context.mounted) {
                            _toast(context, apiError(error));
                          }
                        }
                      },
                    ),
                  ),
                ),
              ),
            ],
            general: [
              ListTile(title: const Text('Code'), subtitle: Text(item.code)),
              ListTile(
                title: const Text('Email'),
                subtitle: Text(item.email.isEmpty ? '—' : item.email),
              ),
              ListTile(
                title: const Text('Phone'),
                subtitle: Text(item.phone.isEmpty ? '—' : item.phone),
              ),
              ListTile(
                title: const Text('Tax number'),
                subtitle: Text(item.taxNumber.isEmpty ? '—' : item.taxNumber),
              ),
              ListTile(
                title: const Text('Address'),
                subtitle: Text(item.address.isEmpty ? '—' : item.address),
              ),
              ListTile(
                title: const Text('City'),
                subtitle: Text(item.city.isEmpty ? '—' : item.city),
              ),
              ListTile(
                title: const Text('Province'),
                subtitle: Text(item.province.isEmpty ? '—' : item.province),
              ),
              ListTile(
                title: const Text('Country'),
                subtitle: Text(item.country.isEmpty ? '—' : item.country),
              ),
              ListTile(
                title: const Text('Postal code'),
                subtitle: Text(item.postalCode.isEmpty ? '—' : item.postalCode),
              ),
            ],
            audit: [
              ListTile(
                title: const Text('Created at'),
                subtitle: Text(item.createdAt),
              ),
              ListTile(
                title: const Text('Updated at'),
                subtitle: Text(item.updatedAt),
              ),
            ],
          ),
        ),
  );
}

// ---------- Form ----------

class SupplierFormPage extends ConsumerStatefulWidget {
  const SupplierFormPage({super.key, this.value});
  final Supplier? value;
  @override
  ConsumerState<SupplierFormPage> createState() => _SupplierFormPageState();
}

class _SupplierFormPageState extends ConsumerState<SupplierFormPage> {
  final _form = GlobalKey<FormState>();
  late final _code = TextEditingController(text: widget.value?.code);
  late final _name = TextEditingController(text: widget.value?.name);
  late final _email = TextEditingController(text: widget.value?.email);
  late final _phone = TextEditingController(text: widget.value?.phone);
  late final _taxNumber = TextEditingController(text: widget.value?.taxNumber);
  late final _address = TextEditingController(text: widget.value?.address);
  late final _city = TextEditingController(text: widget.value?.city);
  late final _province = TextEditingController(text: widget.value?.province);
  late final _country = TextEditingController(text: widget.value?.country);
  late final _postalCode = TextEditingController(text: widget.value?.postalCode);
  bool _loading = false;

  bool get _isEdit => widget.value != null;

  @override
  void dispose() {
    for (final controller in [
      _code,
      _name,
      _email,
      _phone,
      _taxNumber,
      _address,
      _city,
      _province,
      _country,
      _postalCode,
    ]) {
      controller.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    if (!_form.currentState!.validate()) return;
    setState(() => _loading = true);
    final input = SupplierInput(
      code: _code.text.trim(),
      name: _name.text.trim(),
      email: _email.text.trim(),
      phone: _phone.text.trim(),
      taxNumber: _taxNumber.text.trim(),
      address: _address.text.trim(),
      city: _city.text.trim(),
      province: _province.text.trim(),
      country: _country.text.trim(),
      postalCode: _postalCode.text.trim(),
    );
    try {
      final repository = ref.read(supplierRepositoryProvider);
      _isEdit
          ? await repository.update(widget.value!.id, input)
          : await repository.create(input);
      ref.invalidate(supplierListProvider);
      if (!mounted) return;
      _toast(context, _isEdit ? 'Supplier updated' : 'Supplier created');
      Navigator.pop(context);
    } on DioException catch (error) {
      if (mounted) _toast(context, apiError(error));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) => PermissionGuard(
    permission: _isEdit
        ? SupplierPermissions.update
        : SupplierPermissions.create,
    child: MasterFormPage(
      title: _isEdit ? 'Edit supplier' : 'Create supplier',
      loading: _loading,
      child: Form(
        key: _form,
        child: Column(
          children: [
            TextFormField(
              controller: _code,
              enabled: !_isEdit,
              decoration: const InputDecoration(labelText: 'Supplier code'),
              validator: (value) => _required(value, 'Supplier code', min: 2),
            ),
            TextFormField(
              controller: _name,
              decoration: const InputDecoration(labelText: 'Supplier name'),
              validator: (value) => _required(value, 'Supplier name', min: 2),
            ),
            TextFormField(
              controller: _email,
              decoration: const InputDecoration(labelText: 'Email'),
              validator: _optionalEmail,
            ),
            TextFormField(
              controller: _phone,
              decoration: const InputDecoration(labelText: 'Phone'),
            ),
            TextFormField(
              controller: _taxNumber,
              decoration: const InputDecoration(labelText: 'Tax number (NPWP)'),
            ),
            TextFormField(
              controller: _address,
              maxLines: 2,
              decoration: const InputDecoration(labelText: 'Address'),
            ),
            TextFormField(
              controller: _city,
              decoration: const InputDecoration(labelText: 'City'),
            ),
            TextFormField(
              controller: _province,
              decoration: const InputDecoration(labelText: 'Province'),
            ),
            TextFormField(
              controller: _country,
              decoration: const InputDecoration(labelText: 'Country'),
            ),
            TextFormField(
              controller: _postalCode,
              decoration: const InputDecoration(labelText: 'Postal code'),
            ),
            const SizedBox(height: 16),
            AppButton(label: 'Save', loading: _loading, onPressed: _save),
          ],
        ),
      ),
    ),
  );
}

// ---------- Shared helpers ----------

String? _required(String? value, String label, {int min = 1}) {
  final text = value?.trim() ?? '';
  if (text.isEmpty) return '$label is required';
  if (text.length < min) return '$label must be at least $min characters';
  return null;
}

String? _optionalEmail(String? value) {
  final text = value?.trim() ?? '';
  if (text.isEmpty) return null;
  return RegExp(r'^[^@\s]+@[^@\s]+\.[^@\s]+$').hasMatch(text)
      ? null
      : 'Enter a valid email address';
}

void _toast(BuildContext context, String message) =>
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
