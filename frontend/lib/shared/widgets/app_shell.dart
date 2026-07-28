import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/constants/app_spacing.dart';
import '../controllers/theme_controller.dart';

class AppShell extends ConsumerWidget {
  const AppShell({super.key, required this.child});
  final Widget child;

  static const _destinations = [
    NavigationRailDestination(icon: Icon(Icons.dashboard_outlined), selectedIcon: Icon(Icons.dashboard), label: Text('Dashboard')),
    NavigationRailDestination(icon: Icon(Icons.widgets_outlined), selectedIcon: Icon(Icons.widgets), label: Text('Design system')),
  ];

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final compact = MediaQuery.sizeOf(context).width < 760;
    final location = GoRouterState.of(context).uri.path;
    final selectedIndex = location == '/design-system' ? 1 : 0;
    final rail = NavigationRail(
      selectedIndex: selectedIndex,
      labelType: NavigationRailLabelType.all,
      leading: const Padding(
        padding: EdgeInsets.symmetric(vertical: AppSpacing.md),
        child: CircleAvatar(child: Icon(Icons.warehouse_outlined)),
      ),
      destinations: _destinations,
      onDestinationSelected: (index) => context.go(index == 0 ? '/dashboard' : '/design-system'),
    );
    return Scaffold(
      appBar: AppBar(
        titleSpacing: AppSpacing.lg,
        title: Text(location == '/design-system' ? 'Design system' : 'Overview'),
        actions: [
          IconButton(
            tooltip: 'Toggle color theme',
            onPressed: ref.read(themeModeProvider.notifier).toggle,
            icon: const Icon(Icons.dark_mode_outlined),
          ),
          const Padding(
            padding: EdgeInsets.only(right: AppSpacing.md),
            child: Tooltip(message: 'Naufal Mahardika', child: CircleAvatar(radius: 16, child: Text('NM'))),
          ),
        ],
      ),
      drawer: compact
          ? Drawer(child: SafeArea(child: rail))
          : null,
      body: Row(
        children: [
          if (!compact) rail,
          if (!compact) const VerticalDivider(width: 1),
          Expanded(child: child),
        ],
      ),
    );
  }
}
