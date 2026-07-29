import '../entities/lookup_item.dart';

enum LookupType { warehouses, locations, products, suppliers, customers }

abstract interface class LookupRepository {
  Future<List<LookupItem>> getItems(
    LookupType type, {
    String search,
    bool refresh,
  });
}
