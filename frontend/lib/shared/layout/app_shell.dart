import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import 'app_layout.dart';
import 'breadcrumb.dart';
import 'sidebar.dart';
import 'top_navigation.dart';

/// The breadcrumb group and label of each top-level route segment.
///
/// Keyed by the first path segment so a detail or form route inherits its list
/// page's trail automatically. It mirrors the sidebar groups in
/// `navigation_model.dart` — a module is named there and here, nowhere else.
const _sections = <String, ({String group, String label})>{
  'dashboard': (group: 'Workspace', label: 'Dashboard'),
  'warehouses': (group: 'Master data', label: 'Warehouse'),
  'locations': (group: 'Master data', label: 'Location'),
  'products': (group: 'Master data', label: 'Product'),
  'uoms': (group: 'Master data', label: 'UOM'),
  'categories': (group: 'Master data', label: 'Category'),
  'brands': (group: 'Master data', label: 'Brand'),
  'suppliers': (group: 'Master data', label: 'Supplier'),
  'customers': (group: 'Master data', label: 'Customer'),
  'inventory': (group: 'Operations', label: 'Inventory'),
  'inventory-ledger': (group: 'Operations', label: 'Inventory ledger'),
  'design-system': (group: 'Workspace', label: 'Design system'),
};

const _dashboardCrumb = BreadcrumbItem(
  label: 'Dashboard',
  icon: Icons.dashboard_outlined,
);

/// Builds the trail for a location: group → list page → leaf.
List<BreadcrumbItem> breadcrumbFor(String location) {
  final segments = Uri.parse(location).pathSegments;
  final section = segments.isEmpty ? null : _sections[segments.first];
  if (section == null) return const [_dashboardCrumb];

  final items = <BreadcrumbItem>[
    BreadcrumbItem(
      label: section.group,
      icon: Icons.home_outlined,
      location: '/dashboard',
    ),
    BreadcrumbItem(label: section.label, location: '/${segments.first}'),
  ];
  if (segments.length > 1) {
    items.add(
      BreadcrumbItem(
        label: switch (segments[1]) {
          'create' => 'Create',
          'receive' => 'Receive',
          _ => 'Detail',
        },
      ),
    );
  }
  if (segments.length > 2 && segments[2] == 'edit') {
    items.add(const BreadcrumbItem(label: 'Edit'));
  }
  return items;
}

class AppShell extends StatelessWidget {
  const AppShell({super.key, required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final location = GoRouterState.of(context).uri.path;
    return AppLayout(
      topBar: AppTopNavigation(
        breadcrumb: AppBreadcrumb(items: breadcrumbFor(location)),
      ),
      sidebar: AppSidebar(location: location),
      child: child,
    );
  }
}
