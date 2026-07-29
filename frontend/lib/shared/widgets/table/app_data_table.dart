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
    this.currentPage,
    this.totalRecords,
    this.onPageChanged,
    this.sortField,
    this.sortDirection,
    this.sortFields,
    this.onSortChanged,
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

  /// Zero-based page supplied by a server-paginated parent.
  final int? currentPage;

  /// Server-reported record count. When supplied with [onPageChanged], rows
  /// are treated as the current server page rather than locally sliced.
  final int? totalRecords;
  final ValueChanged<int>? onPageChanged;

  /// Externally controlled server-sort field and direction. [sortFields]
  /// maps visible source-column indexes to backend API sort keys.
  final String? sortField;
  final bool? sortDirection;
  final List<String>? sortFields;
  final void Function(String field, bool ascending)? onSortChanged;
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
    if (oldWidget.columns.length != widget.columns.length) {
      _visibleColumns = {
        for (var index = 0; index < widget.columns.length; index++) index,
      };
    }
  }

  @override
  Widget build(BuildContext context) {
    if (widget.loading) return const TableLoading();
    if (widget.errorMessage != null) {
      return AppErrorState(
        message: widget.errorMessage!,
        onRetry: widget.onRetry,
      );
    }
    if (widget.rows.isEmpty) return const TableEmpty();
    final visible = _visibleColumns.toList()..sort();
    final externalSourceIndex =
        widget.sortField == null || widget.sortFields == null
        ? null
        : widget.sortFields!.indexOf(widget.sortField!);
    final externalSortIndex = externalSourceIndex == null
        ? null
        : visible.indexOf(externalSourceIndex);
    final serverPaged =
        widget.totalRecords != null && widget.onPageChanged != null;
    final page = widget.currentPage ?? _page;
    final start = serverPaged
        ? page * widget.pageSize
        : _page * widget.pageSize;
    final pageRows = serverPaged
        ? widget.rows
        : widget.rows.skip(start).take(widget.pageSize).toList();
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
                sortColumnIndex: externalSortIndex == -1
                    ? null
                    : externalSortIndex ?? _sortColumnIndex,
                sortAscending: widget.sortDirection ?? _ascending,
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
          page: page,
          pageSize: widget.pageSize,
          totalRows: widget.totalRecords ?? widget.rows.length,
          onPageChanged: (nextPage) {
            if (serverPaged) {
              widget.onPageChanged!(nextPage);
            } else {
              setState(() => _page = nextPage);
            }
          },
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
              if (widget.onSortChanged == null) {
                setState(() {
                  _sortColumnIndex = columnIndex;
                  _ascending = ascending;
                });
              }
              widget.onSort?.call(index, ascending);
              final field =
                  widget.sortFields != null && index < widget.sortFields!.length
                  ? widget.sortFields![index]
                  : index.toString();
              widget.onSortChanged?.call(field, ascending);
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
