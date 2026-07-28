import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';
import '../feedback/loading_skeleton.dart';
import 'app_card.dart';

class KpiCard extends StatelessWidget {
  const KpiCard({
    super.key,
    required this.title,
    required this.value,
    this.subtitle,
    this.trend,
    this.icon,
    this.loading = false,
  });
  final String title;
  final String value;
  final String? subtitle;
  final String? trend;
  final IconData? icon;
  final bool loading;
  @override
  Widget build(BuildContext context) => AppCard(
    child: loading
        ? const LoadingSkeleton(height: AppSpacing.xl)
        : Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title, style: Theme.of(context).textTheme.labelLarge),
                    const SizedBox(height: AppSpacing.xs),
                    Text(
                      value,
                      style: Theme.of(context).textTheme.headlineSmall,
                    ),
                    if (subtitle != null) Text(subtitle!),
                    if (trend != null)
                      Text(
                        trend!,
                        style: Theme.of(context).textTheme.labelMedium,
                      ),
                  ],
                ),
              ),
              if (icon != null)
                Icon(icon, color: Theme.of(context).colorScheme.primary),
            ],
          ),
  );
}
