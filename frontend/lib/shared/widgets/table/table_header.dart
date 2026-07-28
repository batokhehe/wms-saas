import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';

class TableHeader extends StatelessWidget {
  const TableHeader({
    super.key,
    required this.title,
    this.subtitle,
    this.actions = const [],
  });
  final String title;
  final String? subtitle;
  final List<Widget> actions;
  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: AppSpacing.md),
    child: Wrap(
      alignment: WrapAlignment.spaceBetween,
      runSpacing: AppSpacing.sm,
      children: [
        Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(title, style: Theme.of(context).textTheme.titleMedium),
            if (subtitle != null) Text(subtitle!),
          ],
        ),
        Wrap(spacing: AppSpacing.xs, children: actions),
      ],
    ),
  );
}
