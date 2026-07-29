import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';
import '../buttons/app_button.dart';

class FilterBar extends StatelessWidget {
  const FilterBar({
    super.key,
    this.search,
    this.status,
    this.dateRange,
    this.warehouse,
    this.onReset,
    this.onApply,
  });
  final Widget? search, status, dateRange, warehouse;
  final VoidCallback? onReset, onApply;
  @override
  Widget build(BuildContext context) => Wrap(
    spacing: AppSpacing.sm,
    runSpacing: AppSpacing.sm,
    crossAxisAlignment: WrapCrossAlignment.center,
    children: [
      ?search,
      ?status,
      ?dateRange,
      ?warehouse,
      if (onReset != null)
        TextButton(onPressed: onReset, child: const Text('Reset')),
      if (onApply != null) AppButton(label: 'Apply', onPressed: onApply),
    ],
  );
}
