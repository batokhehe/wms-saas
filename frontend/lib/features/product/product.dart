import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../auth/presentation/controllers/auth_controller.dart';
import '../master/presentation/pages/master_list_page.dart';
import '../master/presentation/widgets/master_states.dart';

class Product {
  const Product({
    required this.id,
    required this.sku,
    required this.name,
    required this.status,
    required this.createdAt,
    this.description = '',
  });
  final String id, sku, name, status, createdAt, description;
  factory Product.fromJson(Map<String, dynamic> json) => Product(
    id: json['id'],
    sku: json['sku'],
    name: json['name'],
    status: json['status'],
    createdAt: json['created_at'] ?? '',
    description: json['description'] ?? '',
  );
}

class ProductRepository {
  ProductRepository(this._api);
  final ApiClient _api;
  Future<List<Product>> list({String search = ''}) async {
    final response = await _api.dio.get(
      '/products',
      queryParameters: search.isEmpty ? null : {'search': search},
    );
    return (response.data['data'] as List)
        .map((item) => Product.fromJson(item))
        .toList();
  }
}

final productRepositoryProvider = Provider(
  (ref) => ProductRepository(ref.read(apiClientProvider)),
);

class ProductListPage extends ConsumerWidget {
  const ProductListPage({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(FutureProvider((ref) => ref.read(productRepositoryProvider).list()));
    return state.when(
      loading: () => const MasterLoading(),
      error: (error, stack) => MasterErrorState(message: '$error'),
      data: (items) => MasterListPage(
        title: 'Products',
        columns: const [
          DataColumn(label: Text('SKU')),
          DataColumn(label: Text('Name')),
          DataColumn(label: Text('Status')),
        ],
        rows: [
          for (final item in items)
            DataRow(cells: [
              DataCell(Text(item.sku)),
              DataCell(Text(item.name)),
              DataCell(Text(item.status)),
            ]),
        ],
      ),
    );
  }
}
