import '../../../../core/network/api_client.dart';
import '../../domain/repositories/lookup_repository.dart';
import '../models/lookup_item_dto.dart';

class LookupRemoteDatasource {
  const LookupRemoteDatasource(this._client);
  final ApiClient _client;
  Future<List<LookupItemDto>> fetch(
    LookupType type, {
    String search = '',
  }) async {
    final response = await _client.dio.get(
      '/lookups/${type.name}',
      queryParameters: search.isEmpty ? null : {'search': search},
    );
    final data = response.data['data'] as List<dynamic>;
    return data
        .map((value) => LookupItemDto.fromJson(value as Map<String, dynamic>))
        .toList();
  }
}
