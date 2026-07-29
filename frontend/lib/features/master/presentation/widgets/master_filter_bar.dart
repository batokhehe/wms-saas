import 'package:flutter/material.dart';

import '../../../../shared/widgets/filters/filter_bar.dart';

class MasterStatusOption<T> {
  const MasterStatusOption({required this.label, required this.value});
  final String label;
  final T value;
}

class MasterFilterBar<T> extends StatelessWidget {
  const MasterFilterBar({
    super.key,
    this.onSearch,
    this.status,
    this.statusFilter,
    this.availableStatuses = const [],
    this.onStatusChanged,
    this.onRefresh,
    this.onClear,
  });
  final ValueChanged<String>? onSearch;
  final Widget? status;
  final T? statusFilter;
  final List<MasterStatusOption<T>> availableStatuses;
  final ValueChanged<T?>? onStatusChanged;
  final VoidCallback? onRefresh;
  final VoidCallback? onClear;
  @override
  Widget build(BuildContext context) => FilterBar(
    search: TextField(
      onChanged: onSearch,
      decoration: const InputDecoration(
        prefixIcon: Icon(Icons.search),
        hintText: 'Search',
      ),
    ),
    status: status ?? _statusDropdown(),
    onApply: onRefresh,
    onReset: onClear,
  );

  Widget? _statusDropdown() {
    if (availableStatuses.isEmpty) return null;
    return DropdownButton<T?>(
      value: statusFilter,
      hint: const Text('Status'),
      items: [
        DropdownMenuItem<T?>(value: null, child: Text('All')),
        ...availableStatuses.map(
          (option) => DropdownMenuItem<T?>(
            value: option.value,
            child: Text(option.label),
          ),
        ),
      ],
      onChanged: onStatusChanged,
    );
  }
}
