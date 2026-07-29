import '../../domain/entities/lookup_item.dart';

class LookupItemDto {
  const LookupItemDto({
    required this.id,
    required this.code,
    required this.name,
  });
  final String id, code, name;
  factory LookupItemDto.fromJson(Map<String, dynamic> json) => LookupItemDto(
    id: json['id'] as String,
    code: json['code'] as String,
    name: json['name'] as String,
  );
  LookupItem toEntity() => LookupItem(id: id, code: code, name: name);
}
