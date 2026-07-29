import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../auth/presentation/controllers/auth_controller.dart';
import '../master/presentation/pages/master_list_page.dart';
import '../master/presentation/widgets/master_states.dart';
import '../master/presentation/widgets/status_chip.dart';
import '../../shared/widgets/status/status_badge.dart';

// UOM Model
class UOM {
  final String id, code, name, status, createdAt;
  const UOM({required this.id, required this.code, required this.name, required this.status, required this.createdAt});
  factory UOM.fromJson(Map<String, dynamic> json) => UOM(id: json['id'], code: json['code'], name: json['name'], status: json['status'], createdAt: json['created_at']);
}

class UOMRepository {
  UOMRepository(this._api);
  final ApiClient _api;
  Future<List<UOM>> list() async {
    final response = await _api.dio.get('/uoms');
    return (response.data['data'] as List).map((item) => UOM.fromJson(item)).toList();
  }
}

final uomRepositoryProvider = Provider((ref) => UOMRepository(ref.read(apiClientProvider)));

class UOMListPage extends ConsumerWidget {
  const UOMListPage({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(FutureProvider((ref) => ref.read(uomRepositoryProvider).list()));
    return state.when(
      loading: () => const MasterLoading(),
      error: (e, _) => MasterErrorState(message: '$e'),
      data: (items) => MasterListPage(
        title: 'Units of Measure',
        columns: const [DataColumn(label: Text('Code')), DataColumn(label: Text('Name')), DataColumn(label: Text('Status'))],
        rows: [for (final item in items) DataRow(cells: [DataCell(Text(item.code)), DataCell(Text(item.name)), DataCell(StatusChip(status: item.status == 'ACTIVE' ? AppStatus.active : AppStatus.inactive))])],
      ),
    );
  }
}
