import '../entities/lookup_item.dart';
import '../repositories/lookup_repository.dart';

class GetLookupItems {
  const GetLookupItems(this._repository);
  final LookupRepository _repository;
  Future<List<LookupItem>> call(
    LookupType type, {
    String search = '',
    bool refresh = false,
  }) => _repository.getItems(type, search: search, refresh: refresh);
}
