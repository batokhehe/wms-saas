import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';
import '../feedback/loading_skeleton.dart';

class TableLoading extends StatelessWidget {
  const TableLoading({super.key, this.rows = 5});
  final int rows;
  @override
  Widget build(BuildContext context) => ListView.separated(
    itemCount: rows,
    itemBuilder: (context, index) =>
        const LoadingSkeleton(height: AppSpacing.lg),
    separatorBuilder: (context, index) => const SizedBox(height: AppSpacing.sm),
  );
}
