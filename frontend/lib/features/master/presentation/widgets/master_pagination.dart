import 'package:flutter/material.dart';

import '../../../../shared/widgets/table/table_pagination.dart';

class MasterPagination extends StatelessWidget {
  const MasterPagination({
    super.key,
    required this.page,
    required this.pageSize,
    required this.totalRows,
    required this.onPageChanged,
  });
  final int page, pageSize, totalRows;
  final ValueChanged<int> onPageChanged;
  @override
  Widget build(BuildContext context) => TablePagination(
    page: page,
    pageSize: pageSize,
    totalRows: totalRows,
    onPageChanged: onPageChanged,
  );
}
