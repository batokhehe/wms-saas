import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';

class TablePagination extends StatelessWidget {
  const TablePagination({
    super.key,
    required this.page,
    required this.pageSize,
    required this.totalRows,
    required this.onPageChanged,
  });
  final int page, pageSize, totalRows;
  final ValueChanged<int> onPageChanged;
  int get _pageCount => totalRows == 0 ? 1 : (totalRows / pageSize).ceil();
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(top: AppSpacing.md),
    child: Row(
      mainAxisAlignment: MainAxisAlignment.end,
      children: [
        Text('Page ${page + 1} of $_pageCount'),
        IconButton(
          tooltip: 'Previous page',
          onPressed: page == 0 ? null : () => onPageChanged(page - 1),
          icon: const Icon(Icons.chevron_left),
        ),
        IconButton(
          tooltip: 'Next page',
          onPressed: page + 1 >= _pageCount
              ? null
              : () => onPageChanged(page + 1),
          icon: const Icon(Icons.chevron_right),
        ),
      ],
    ),
  );
}
