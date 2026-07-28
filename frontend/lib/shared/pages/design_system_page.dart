import 'package:flutter/material.dart';

import '../../core/constants/app_spacing.dart';
import '../widgets/app_components.dart';

class DesignSystemPage extends StatelessWidget {
  const DesignSystemPage({super.key});
  @override
  Widget build(BuildContext context) => ListView(padding: AppSpacing.page, children: [
    Text('Reusable UI foundation', style: Theme.of(context).textTheme.bodyLarge), const SizedBox(height: AppSpacing.lg),
    AppSectionCard(title: 'Actions', child: Wrap(spacing: AppSpacing.sm, runSpacing: AppSpacing.sm, children: [FilledButton(onPressed: () {}, child: const Text('Primary action')), OutlinedButton(onPressed: () {}, child: const Text('Secondary action')), TextButton(onPressed: () {}, child: const Text('Tertiary action'))])),
    const SizedBox(height: AppSpacing.md),
    const AppSectionCard(title: 'Statuses', child: Wrap(spacing: AppSpacing.sm, children: [StatusBadge(label: 'Available', tone: StatusTone.success), StatusBadge(label: 'Attention', tone: StatusTone.warning), StatusBadge(label: 'Blocked', tone: StatusTone.danger), StatusBadge(label: 'Draft')])),
    const SizedBox(height: AppSpacing.md),
    const AppSectionCard(title: 'Loading', child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [LoadingSkeleton(), SizedBox(height: AppSpacing.sm), LoadingSkeleton(height: 28)])),
    const SizedBox(height: AppSpacing.md),
    AppSectionCard(title: 'Empty state', child: SizedBox(height: 220, child: EmptyState(title: 'No records yet', description: 'Results will appear here when they are available.', action: OutlinedButton(onPressed: () {}, child: const Text('Clear filters'))))),
  ]);
}
