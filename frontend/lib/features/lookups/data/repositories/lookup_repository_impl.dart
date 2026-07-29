import '../../domain/entities/lookup_item.dart';
import '../../domain/repositories/lookup_repository.dart';
import '../datasources/lookup_remote_datasource.dart';

class LookupRepositoryImpl implements LookupRepository {
  LookupRepositoryImpl(this._remote);
  final LookupRemoteDatasource _remote;
  final Map<String, Future<List<LookupItem>>> _cache = {};
  @override
  Future<List<LookupItem>> getItems(
    LookupType type, {
    String search = '',
    bool refresh = false,
  }) {
    final key = '${type.name}:$search';
    if (refresh) _cache.remove(key);
    return _cache.putIfAbsent(
      key,
      () async => (await _remote.fetch(
        type,
        search: search,
      )).map((dto) => dto.toEntity()).toList(),
    );
  }
}
