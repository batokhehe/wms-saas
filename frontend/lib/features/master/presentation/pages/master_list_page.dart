import 'package:flutter/material.dart';

import '../../../../shared/layout/page_layout.dart';
import '../../../../shared/widgets/table/app_data_table.dart';
import '../widgets/master_filter_bar.dart';

class MasterListPage<T> extends StatelessWidget {
  const MasterListPage({
    super.key,
    required this.title,
    required this.columns,
    required this.rows,
    this.actions = const [],
    this.filters,
    this.loading = false,
    this.errorMessage,
    this.onRetry,
    this.onSearch,
    this.currentPage,
    this.pageSize = 10,
    this.totalRecords,
    this.onPageChanged,
    this.sortField,
    this.sortDirection,
    this.sortFields,
    this.onSortChanged,
    this.statusFilter,
    this.availableStatuses = const [],
    this.onStatusChanged,
  });
  final String title;
  final List<DataColumn> columns;
  final List<DataRow> rows;
  final List<Widget> actions;
  final Widget? filters;
  final bool loading;
  final String? errorMessage;
  final VoidCallback? onRetry;
  final ValueChanged<String>? onSearch;
  final int? currentPage;
  final int pageSize;
  final int? totalRecords;
  final ValueChanged<int>? onPageChanged;
  final String? sortField;
  final bool? sortDirection;
  final List<String>? sortFields;
  final void Function(String field, bool ascending)? onSortChanged;
  final T? statusFilter;
  final List<MasterStatusOption<T>> availableStatuses;
  final ValueChanged<T?>? onStatusChanged;
  @override
  Widget build(BuildContext context) => AppPage(
    title: title,
    actions: actions,
    body: AppDataTable(
      title: title,
      columns: columns,
      rows: rows,
      filters:
          filters ??
          (availableStatuses.isEmpty
              ? null
              : MasterFilterBar<T>(
                  statusFilter: statusFilter,
                  availableStatuses: availableStatuses,
                  onStatusChanged: onStatusChanged,
                )),
      loading: loading,
      errorMessage: errorMessage,
      onRetry: onRetry,
      onSearch: onSearch,
      currentPage: currentPage,
      pageSize: pageSize,
      totalRecords: totalRecords,
      onPageChanged: onPageChanged,
      sortField: sortField,
      sortDirection: sortDirection,
      sortFields: sortFields,
      onSortChanged: onSortChanged,
    ),
  );
}
