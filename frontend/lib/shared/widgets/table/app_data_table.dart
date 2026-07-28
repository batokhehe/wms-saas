import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';
import '../feedback/error_state.dart';
import 'table_empty.dart';
import 'table_header.dart';
import 'table_loading.dart';
import 'table_pagination.dart';
import 'table_toolbar.dart';

class AppDataTable extends StatefulWidget {
  const AppDataTable({
    super.key,
    required this.title,
    required this.columns,
    required this.rows,
    this.subtitle,
    this.filters,
    this.loading = false,
    this.errorMessage,
    this.onRetry,
    this.onSearch,
    this.onSort,
    this.onExport,
    this.onBulkAction,
    this.onColumnResize,
    this.pageSize = 10,
  });
  final String title;
  final String? subtitle, errorMessage;
  final List<DataColumn> columns;
  final List<DataRow> rows;
  final Widget? filters;
  final bool loading;
  final VoidCallback? onRetry, onExport, onBulkAction, onColumnResize;
  final ValueChanged<String>? onSearch;
  final void Function(int columnIndex, bool ascending)? onSort;
  final int pageSize;
  @override
  State<AppDataTable> createState() => _AppDataTableState();
}

class _AppDataTableState extends State<AppDataTable> {
  final Set<int> _selected = {};
  late Set<int> _visibleColumns;
  int _page = 0;
  bool _ascending = true;
  int? _sortColumnIndex;
  @override
  void initState() {
    super.initState();
    _visibleColumns = {
      for (var index = 0; index < widget.columns.length; index++) index,
    };
  }

  @override
  void didUpdateWidget(covariant AppDataTable oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.columns.length != widget.columns.length)
      _visibleColumns = {
        for (var index = 0; index < widget.columns.length; index++) index,
      };
  }

  @override
  Widget build(BuildContext context) {
    if (widget.loading) return const TableLoading();
    if (widget.errorMessage != null)
      return AppErrorState(
        message: widget.errorMessage!,
        onRetry: widget.onRetry,
      );
    if (widget.rows.isEmpty) return const TableEmpty();
    final visible = _visibleColumns.toList()..sort();
    final start = _page * widget.pageSize;
    final pageRows = widget.rows.skip(start).take(widget.pageSize).toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        TableHeader(title: widget.title, subtitle: widget.subtitle),
        TableToolbar(
          onSearch: widget.onSearch,
          filters: widget.filters,
          selectedCount: _selected.length,
          onExport: widget.onExport,
          onBulkAction: widget.onBulkAction,
          onColumnResize: widget.onColumnResize,
          onColumnVisibility: () => _showColumnVisibility(context),
        ),
        const SizedBox(height: AppSpacing.md),
        Expanded(
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: SingleChildScrollView(
              child: DataTable(
                showCheckboxColumn: true,
                onSelectAll: (selected) =>
                    _selectPage(selected == true, start, pageRows.length),
                sortColumnIndex: _sortColumnIndex,
                sortAscending: _ascending,
                columns: [for (final index in visible) _columnFor(index)],
                rows: [
                  for (var offset = 0; offset < pageRows.length; offset++)
                    _rowFor(start + offset, pageRows[offset], visible),
                ],
              ),
            ),
          ),
        ),
        TablePagination(
          page: _page,
          pageSize: widget.pageSize,
          totalRows: widget.rows.length,
          onPageChanged: (page) => setState(() => _page = page),
        ),
      ],
    );
  }

  DataColumn _columnFor(int index) {
    final source = widget.columns[index];
    return DataColumn(
      label: source.label,
      tooltip: source.tooltip,
      numeric: source.numeric,
      onSort: source.onSort == null
          ? null
          : (columnIndex, ascending) {
              setState(() {
                _sortColumnIndex = columnIndex;
                _ascending = ascending;
              });
              widget.onSort?.call(index, ascending);
            },
    );
  }

  DataRow _rowFor(int index, DataRow source, List<int> visible) =>
      DataRow.byIndex(
        index: index,
        selected: _selected.contains(index),
        onSelectChanged: (selected) => setState(() {
          selected == true ? _selected.add(index) : _selected.remove(index);
        }),
        cells: [for (final columnIndex in visible) source.cells[columnIndex]],
      );
  void _selectPage(bool selected, int start, int count) => setState(() {
    for (var index = start; index < start + count; index++) {
      selected ? _selected.add(index) : _selected.remove(index);
    }
  });
  void _showColumnVisibility(BuildContext context) => showDialog<void>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('Visible columns'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          for (var index = 0; index < widget.columns.length; index++)
            CheckboxListTile(
              value: _visibleColumns.contains(index),
              title: widget.columns[index].label,
              onChanged: (visible) => setState(() {
                visible == true
                    ? _visibleColumns.add(index)
                    : _visibleColumns.remove(index);
              }),
            ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Done'),
        ),
      ],
    ),
  );
}
