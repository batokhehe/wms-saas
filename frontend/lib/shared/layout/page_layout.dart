import 'package:flutter/material.dart';

import '../../core/constants/app_spacing.dart';
import 'content_layout.dart';

class AppPage extends StatelessWidget {
  const AppPage({
    super.key,
    required this.title,
    required this.body,
    this.subtitle,
    this.actions = const [],
    this.filters,
    this.search,
    this.floatingActionButton,
    this.loading = false,
  });

  final String title;
  final String? subtitle;
  final List<Widget> actions;
  final Widget? filters;
  final Widget? search;
  final Widget body;
  final Widget? floatingActionButton;
  final bool loading;

  @override
  Widget build(BuildContext context) => Stack(
    children: [
      ContentLayout(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Wrap(
              alignment: WrapAlignment.spaceBetween,
              crossAxisAlignment: WrapCrossAlignment.center,
              runSpacing: AppSpacing.md,
              spacing: AppSpacing.md,
              children: [
                _PageHeading(title: title, subtitle: subtitle),
                Wrap(
                  spacing: AppSpacing.sm,
                  runSpacing: AppSpacing.sm,
                  children: actions,
                ),
              ],
            ),
            if (filters != null || search != null) ...[
              const SizedBox(height: AppSpacing.lg),
              Wrap(
                spacing: AppSpacing.md,
                runSpacing: AppSpacing.sm,
                children: [
                  if (search != null) search!,
                  if (filters != null) filters!,
                ],
              ),
            ],
            const SizedBox(height: AppSpacing.lg),
            Expanded(child: body),
          ],
        ),
      ),
      if (floatingActionButton != null)
        Positioned(
          right: AppSpacing.lg,
          bottom: AppSpacing.lg,
          child: floatingActionButton!,
        ),
      if (loading) const _LoadingOverlay(),
    ],
  );
}

class PageLayout extends AppPage {
  const PageLayout({
    super.key,
    required super.title,
    required super.body,
    super.subtitle,
    super.actions,
    super.filters,
    super.search,
    super.floatingActionButton,
    super.loading,
  });
}

class _PageHeading extends StatelessWidget {
  const _PageHeading({required this.title, this.subtitle});
  final String title;
  final String? subtitle;
  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    mainAxisSize: MainAxisSize.min,
    children: [
      Text(title, style: Theme.of(context).textTheme.headlineSmall),
      if (subtitle != null) ...[
        const SizedBox(height: AppSpacing.xs),
        Text(subtitle!, style: Theme.of(context).textTheme.bodyLarge),
      ],
    ],
  );
}

class _LoadingOverlay extends StatelessWidget {
  const _LoadingOverlay();
  @override
  Widget build(BuildContext context) => Positioned.fill(
    child: ColoredBox(
      color: Theme.of(context).colorScheme.scrim.withValues(alpha: 0.16),
      child: const Center(child: CircularProgressIndicator()),
    ),
  );
}
