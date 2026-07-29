import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/network/api_client.dart' as network;
import '../master/presentation/pages/master_list_page.dart';
import '../master/presentation/widgets/master_states.dart';
import '../master/presentation/widgets/status_chip.dart';
import '../../shared/widgets/status/status_badge.dart';

class Brand {
  final String id, code, name, status, createdAt;
  const Brand({required this.id, required this.code, required this.name, required this.status, required this.createdAt});
  factory Brand.fromJson(Map<String, dynamic> json) => Brand(id: json['id'], code: json['code'], name: json['name'], status: json['status'], createdAt: json['created_at']);
}

class BrandRepository {
  BrandRepository(this._api);
  final network.ApiClient _api;
  Future<List<Brand>> list() async {
    final response = await _api.dio.get('/brands');
    return (response.data['data'] as List).map((item) => Brand.fromJson(item)).toList();
  }
}

final brandRepositoryProvider = Provider((ref) => BrandRepository(ref.read(network.apiClientProvider)));

class BrandListPage extends ConsumerWidget {
  const BrandListPage({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(FutureProvider((ref) => ref.read(brandRepositoryProvider).list()));
    return state.when(
      loading: () => const MasterLoading(),
      error: (e, _) => MasterErrorState(message: '$e'),
      data: (items) => MasterListPage(
        title: 'Brands',
        columns: const [DataColumn(label: Text('Code')), DataColumn(label: Text('Name')), DataColumn(label: Text('Status'))],
        rows: [for (final item in items) DataRow(cells: [DataCell(Text(item.code)), DataCell(Text(item.name)), DataCell(StatusChip(status: item.status == 'ACTIVE' ? AppStatus.active : AppStatus.inactive))])],
      ),
    );
  }
}
