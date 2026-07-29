class LookupItem {
  const LookupItem({required this.id, required this.code, required this.name});
  final String id;
  final String code;
  final String name;
  String get label => '$code — $name';
}
