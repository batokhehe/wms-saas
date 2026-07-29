import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../core/constants/app_spacing.dart';

class BreadcrumbItem {
  const BreadcrumbItem({required this.label, this.icon, this.location});
  final String label;
  final IconData? icon;
  final String? location;
}

class AppBreadcrumb extends StatelessWidget {
  const AppBreadcrumb({super.key, required this.items});
  final List<BreadcrumbItem> items;

  @override
  Widget build(BuildContext context) {
    final visibleItems = items.length > 3
        ? [items.first, const BreadcrumbItem(label: '…'), items.last]
        : items;
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: Row(
        children: [
          for (var index = 0; index < visibleItems.length; index++) ...[
            if (index > 0)
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: AppSpacing.xs),
                child: Icon(Icons.chevron_right, size: AppSpacing.md),
              ),
            _BreadcrumbLink(
              item: visibleItems[index],
              current: index == visibleItems.length - 1,
            ),
          ],
        ],
      ),
    );
  }
}

class _BreadcrumbLink extends StatelessWidget {
  const _BreadcrumbLink({required this.item, required this.current});
  final BreadcrumbItem item;
  final bool current;
  @override
  Widget build(BuildContext context) {
    final label = Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (item.icon != null) ...[
          Icon(item.icon, size: AppSpacing.md),
          const SizedBox(width: AppSpacing.xs),
        ],
        Text(item.label),
      ],
    );
    if (current || item.location == null) {
      return DefaultTextStyle.merge(
        style: Theme.of(context).textTheme.bodyMedium,
        child: label,
      );
    }
    return TextButton(
      onPressed: () => context.go(item.location!),
      child: label,
    );
  }
}
