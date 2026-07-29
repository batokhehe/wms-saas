import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/master_query.dart';

class MasterListController extends Notifier<MasterQuery> {
  @override
  MasterQuery build() => const MasterQuery();
  void search(String value) => state = state.copyWith(search: value, page: 1);
  void status(String value) => state = state.copyWith(status: value, page: 1);
  void page(int value) => state = state.copyWith(page: value);
  void clear() => state = const MasterQuery();
}
