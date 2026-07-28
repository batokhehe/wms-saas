import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';

class FilterChipOption<T> {
  const FilterChipOption({required this.label, required this.value});
  final String label;
  final T value;
}

class FilterChipGroup<T> extends StatelessWidget {
  const FilterChipGroup({
    super.key,
    required this.options,
    required this.selected,
    required this.onSelected,
  });
  final List<FilterChipOption<T>> options;
  final Set<T> selected;
  final ValueChanged<Set<T>> onSelected;
  @override
  Widget build(BuildContext context) => Wrap(
    spacing: AppSpacing.xs,
    runSpacing: AppSpacing.xs,
    children: [
      for (final option in options)
        FilterChip(
          label: Text(option.label),
          selected: selected.contains(option.value),
          onSelected: (value) {
            final next = {...selected};
            value ? next.add(option.value) : next.remove(option.value);
            onSelected(next);
          },
        ),
    ],
  );
}
