import 'package:flutter/material.dart';

import '../../core/constants/app_spacing.dart';
import '../../core/constants/app_radius.dart';

enum StatusTone { success, warning, danger, neutral }

class StatusBadge extends StatelessWidget {
  const StatusBadge({super.key, required this.label, this.tone = StatusTone.neutral});
  final String label;
  final StatusTone tone;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final color = switch (tone) {
      StatusTone.success => colors.primary,
      StatusTone.warning => colors.tertiary,
      StatusTone.danger => colors.error,
      StatusTone.neutral => colors.secondary,
    };
    return Chip(
      avatar: Icon(Icons.circle, size: 8, color: color),
      label: Text(label),
      visualDensity: VisualDensity.compact,
      side: BorderSide.none,
    );
  }
}

class AppSearchField extends StatelessWidget {
  const AppSearchField({super.key, this.hintText = 'Search', this.onChanged});
  final String hintText;
  final ValueChanged<String>? onChanged;
  @override
  Widget build(BuildContext context) => SizedBox(
        width: 280,
        child: TextField(
          onChanged: onChanged,
          decoration: InputDecoration(prefixIcon: const Icon(Icons.search), hintText: hintText),
        ),
      );
}

class AppSectionCard extends StatelessWidget {
  const AppSectionCard({super.key, required this.title, required this.child, this.action});
  final String title;
  final Widget child;
  final Widget? action;
  @override
  Widget build(BuildContext context) => Card(
        child: Padding(
          padding: AppSpacing.card,
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Row(children: [Expanded(child: Text(title, style: Theme.of(context).textTheme.titleMedium)), if (action != null) action!]),
            const SizedBox(height: AppSpacing.md),
            child,
          ]),
        ),
      );
}

class EmptyState extends StatelessWidget {
  const EmptyState({super.key, required this.title, required this.description, this.action});
  final String title;
  final String description;
  final Widget? action;
  @override
  Widget build(BuildContext context) => Center(child: Padding(
    padding: const EdgeInsets.all(AppSpacing.xxl),
    child: Column(mainAxisSize: MainAxisSize.min, children: [
      const Icon(Icons.inventory_2_outlined, size: 48), const SizedBox(height: AppSpacing.md),
      Text(title, style: Theme.of(context).textTheme.titleMedium), const SizedBox(height: AppSpacing.xs),
      Text(description, textAlign: TextAlign.center), if (action != null) ...[const SizedBox(height: AppSpacing.md), action!],
    ]),
  ));
}

class ErrorState extends StatelessWidget {
  const ErrorState({super.key, required this.message, required this.onRetry});
  final String message;
  final VoidCallback onRetry;
  @override
  Widget build(BuildContext context) => EmptyState(title: 'Something went wrong', description: message, action: FilledButton.icon(onPressed: onRetry, icon: const Icon(Icons.refresh), label: const Text('Retry')));
}

class LoadingSkeleton extends StatelessWidget {
  const LoadingSkeleton({super.key, this.height = 16});
  final double height;
  @override
  Widget build(BuildContext context) => Container(height: height, decoration: BoxDecoration(color: Theme.of(context).colorScheme.surfaceContainerHighest, borderRadius: BorderRadius.circular(AppRadius.input)));
}
