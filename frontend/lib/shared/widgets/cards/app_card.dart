import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';

class AppCard extends StatelessWidget {
  const AppCard({
    super.key,
    required this.child,
    this.title,
    this.action,
    this.padding = AppSpacing.card,
  });
  final Widget child;
  final String? title;
  final Widget? action;
  final EdgeInsetsGeometry padding;
  @override
  Widget build(BuildContext context) => Card(
    child: Padding(
      padding: padding,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (title != null)
            Row(
              children: [
                Expanded(
                  child: Text(
                    title!,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
                ?action,
              ],
            ),
          if (title != null) const SizedBox(height: AppSpacing.md),
          child,
        ],
      ),
    ),
  );
}
