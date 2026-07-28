import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import 'app_layout.dart';
import 'breadcrumb.dart';
import 'sidebar.dart';
import 'top_navigation.dart';

class AppShell extends StatelessWidget {
  const AppShell({super.key, required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final location = GoRouterState.of(context).uri.path;
    final breadcrumb = switch (location) {
      '/design-system' => const [
        BreadcrumbItem(
          label: 'Workspace',
          icon: Icons.home_outlined,
          location: '/dashboard',
        ),
        BreadcrumbItem(label: 'Design system'),
      ],
      _ => const [
        BreadcrumbItem(label: 'Dashboard', icon: Icons.dashboard_outlined),
      ],
    };
    return AppLayout(
      topBar: AppTopNavigation(breadcrumb: AppBreadcrumb(items: breadcrumb)),
      sidebar: AppSidebar(location: location),
      child: child,
    );
  }
}
