import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../core/constants/app_spacing.dart';
import '../../core/constants/app_motion.dart';
import 'navigation_model.dart';
import 'responsive_layout.dart';

class AppSidebar extends StatefulWidget {
  const AppSidebar({super.key, required this.location});
  final String location;
  @override
  State<AppSidebar> createState() => _AppSidebarState();
}

class _AppSidebarState extends State<AppSidebar> {
  bool _collapsed = false;

  @override
  Widget build(BuildContext context) {
    final compact = ResponsiveLayout.viewportOf(context) == AppViewport.tablet;
    final collapsed = _collapsed || compact;
    return AnimatedContainer(
      duration: AppMotion.standard,
      width: collapsed ? AppSpacing.xxxl : AppSpacing.xxxl * 4,
      child: Material(
        color: Theme.of(context).colorScheme.surface,
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.all(AppSpacing.md),
              child: Row(
                children: [
                  const CircleAvatar(child: Icon(Icons.warehouse_outlined)),
                  if (!collapsed) ...[
                    const SizedBox(width: AppSpacing.sm),
                    Expanded(
                      child: Text(
                        'WMS SaaS',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                    ),
                  ],
                  if (!compact)
                    IconButton(
                      tooltip: collapsed
                          ? 'Expand navigation'
                          : 'Collapse navigation',
                      onPressed: () => setState(() => _collapsed = !_collapsed),
                      icon: Icon(
                        collapsed ? Icons.chevron_right : Icons.chevron_left,
                      ),
                    ),
                ],
              ),
            ),
            Expanded(
              child: ListView(
                children: [
                  for (final group in appNavigationGroups)
                    _SidebarGroup(
                      group: group,
                      location: widget.location,
                      collapsed: collapsed,
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SidebarGroup extends StatelessWidget {
  const _SidebarGroup({
    required this.group,
    required this.location,
    required this.collapsed,
  });
  final AppNavigationGroup group;
  final String location;
  final bool collapsed;
  @override
  Widget build(BuildContext context) {
    if (collapsed) {
      return Column(
        children: [
          for (final item in group.items)
            _SidebarItem(item: item, location: location, collapsed: true),
        ],
      );
    }
    return ExpansionTile(
      initiallyExpanded: group.items.any((item) => item.location == location),
      title: Text(group.label, style: Theme.of(context).textTheme.labelMedium),
      children: [
        for (final item in group.items)
          _SidebarItem(item: item, location: location, collapsed: false),
      ],
    );
  }
}

class _SidebarItem extends StatelessWidget {
  const _SidebarItem({
    required this.item,
    required this.location,
    required this.collapsed,
  });
  final AppNavigationItem item;
  final String location;
  final bool collapsed;
  @override
  Widget build(BuildContext context) {
    final selected = item.location == location;
    return Tooltip(
      message: collapsed ? item.label : '',
      child: ListTile(
        selected: selected,
        selectedTileColor: Theme.of(context).colorScheme.secondaryContainer,
        leading: Icon(item.icon),
        title: collapsed ? null : Text(item.label),
        trailing: collapsed || item.badge == null
            ? null
            : Badge(label: Text(item.badge!)),
        onTap: () {
          if (item.location != null) {
            context.go(item.location!);
          } else {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text('${item.label} is prepared for a future module.'),
              ),
            );
          }
        },
      ),
    );
  }
}
