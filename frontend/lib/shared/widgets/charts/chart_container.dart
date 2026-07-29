import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';
import '../cards/app_card.dart';
import '../feedback/empty_state.dart';
import '../feedback/loading_skeleton.dart';

class ChartContainer extends StatelessWidget {
  const ChartContainer({
    super.key,
    required this.title,
    required this.child,
    this.actions,
    this.filter,
    this.loading = false,
    this.empty = false,
    this.onRefresh,
  });
  final String title;
  final Widget child;
  final Widget? actions, filter;
  final bool loading, empty;
  final VoidCallback? onRefresh;
  @override
  Widget build(BuildContext context) => AppCard(
    title: title,
    action: Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        ?filter,
        if (onRefresh != null)
          IconButton(
            tooltip: 'Refresh',
            onPressed: onRefresh,
            icon: const Icon(Icons.refresh),
          ),
        ?actions,
      ],
    ),
    child: SizedBox(
      height: AppSpacing.xxxl * 3,
      child: loading
          ? const LoadingSkeleton()
          : empty
          ? const AppEmptyState(
              title: 'No chart data',
              description: 'Data will appear when available.',
            )
          : child,
    ),
  );
}
