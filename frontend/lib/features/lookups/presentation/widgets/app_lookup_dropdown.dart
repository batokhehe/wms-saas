import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../domain/entities/lookup_item.dart';
import '../../domain/repositories/lookup_repository.dart';
import 'searchable_lookup_dropdown.dart';

class AppLookupDropdown extends ConsumerWidget {
  const AppLookupDropdown({
    super.key,
    required this.type,
    required this.label,
    this.value,
    this.onChanged,
    this.enabled = true,
    this.readOnly = false,
    this.allowClear = true,
  });
  final LookupType type;
  final String label;
  final LookupItem? value;
  final ValueChanged<LookupItem?>? onChanged;
  final bool enabled, readOnly, allowClear;
  @override
  Widget build(BuildContext context, WidgetRef ref) => InputDecorator(
    decoration: InputDecoration(
      labelText: label,
      suffixIcon: value != null && allowClear
          ? IconButton(
              tooltip: 'Clear',
              onPressed: enabled && !readOnly
                  ? () => onChanged?.call(null)
                  : null,
              icon: const Icon(Icons.clear),
            )
          : null,
    ),
    child: InkWell(
      onTap: !enabled || readOnly
          ? null
          : () async {
              final item = await showDialog<LookupItem>(
                context: context,
                builder: (_) =>
                    SearchableLookupDropdown(type: type, initialValue: value),
              );
              if (item != null) onChanged?.call(item);
            },
      child: Row(
        children: [
          Expanded(child: Text(value?.label ?? 'Select $label')),
          const Icon(Icons.arrow_drop_down),
        ],
      ),
    ),
  );
}
