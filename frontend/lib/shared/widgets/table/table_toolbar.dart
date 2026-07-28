import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';
import '../forms/app_search_field.dart';

class TableToolbar extends StatelessWidget {
  const TableToolbar({
    super.key,
    this.onSearch,
    this.filters,
    this.selectedCount = 0,
    this.onExport,
    this.onColumnVisibility,
    this.onColumnResize,
    this.onBulkAction,
  });
  final ValueChanged<String>? onSearch;
  final Widget? filters;
  final int selectedCount;
  final VoidCallback? onExport,
      onColumnVisibility,
      onColumnResize,
      onBulkAction;
  @override
  Widget build(BuildContext context) => Wrap(
    spacing: AppSpacing.sm,
    runSpacing: AppSpacing.sm,
    crossAxisAlignment: WrapCrossAlignment.center,
    children: [
      SizedBox(
        width: AppSpacing.xxxl * 4,
        child: AppSearchField(onChanged: onSearch),
      ),
      if (filters != null) filters!,
      if (selectedCount > 0) Text('$selectedCount selected'),
      if (onBulkAction != null && selectedCount > 0)
        TextButton(onPressed: onBulkAction, child: const Text('Bulk actions')),
      if (onColumnVisibility != null)
        IconButton(
          tooltip: 'Column visibility',
          onPressed: onColumnVisibility,
          icon: const Icon(Icons.view_column_outlined),
        ),
      if (onColumnResize != null)
        IconButton(
          tooltip: 'Column resize',
          onPressed: onColumnResize,
          icon: const Icon(Icons.width_normal_outlined),
        ),
      if (onExport != null)
        OutlinedButton.icon(
          onPressed: onExport,
          icon: const Icon(Icons.file_download_outlined),
          label: const Text('Export'),
        ),
    ],
  );
}
