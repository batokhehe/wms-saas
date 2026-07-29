class MasterQuery {
  const MasterQuery({this.search = '', this.status = '', this.page = 1});
  final String search;
  final String status;
  final int page;

  MasterQuery copyWith({String? search, String? status, int? page}) =>
      MasterQuery(
        search: search ?? this.search,
        status: status ?? this.status,
        page: page ?? this.page,
      );
}
