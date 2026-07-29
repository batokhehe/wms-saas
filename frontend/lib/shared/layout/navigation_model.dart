import 'package:flutter/material.dart';

class AppNavigationItem {
  const AppNavigationItem({
    required this.label,
    required this.icon,
    this.location,
    this.badge,
  });
  final String label;
  final IconData icon;
  final String? location;
  final String? badge;
}

class AppNavigationGroup {
  const AppNavigationGroup({required this.label, required this.items});
  final String label;
  final List<AppNavigationItem> items;
}

const appNavigationGroups = [
  AppNavigationGroup(
    label: 'Workspace',
    items: [
      AppNavigationItem(
        label: 'Dashboard',
        icon: Icons.dashboard_outlined,
        location: '/dashboard',
      ),
    ],
  ),
  AppNavigationGroup(
    label: 'Master data',
    items: [
      AppNavigationItem(
        label: 'Warehouse',
        icon: Icons.warehouse_outlined,
        location: '/warehouses',
      ),
      AppNavigationItem(
        label: 'Location',
        icon: Icons.grid_view_outlined,
        location: '/locations',
      ),
      AppNavigationItem(
        label: 'Product',
        icon: Icons.inventory_2_outlined,
        location: '/products',
      ),
      AppNavigationItem(
        label: 'UOM',
        icon: Icons.straighten_outlined,
        location: '/uoms',
      ),
      AppNavigationItem(
        label: 'Category',
        icon: Icons.category_outlined,
        location: '/categories',
      ),
      AppNavigationItem(
        label: 'Brand',
        icon: Icons.branding_watermark_outlined,
        location: '/brands',
      ),
      AppNavigationItem(
        label: 'Supplier',
        icon: Icons.local_shipping_outlined,
        location: '/suppliers',
      ),
      AppNavigationItem(
        label: 'Customer',
        icon: Icons.people_outline,
        location: '/customers',
      ),
    ],
  ),
  AppNavigationGroup(
    label: 'Inbound',
    items: [
      AppNavigationItem(
        label: 'Purchase order',
        icon: Icons.shopping_cart_outlined,
      ),
      AppNavigationItem(label: 'ASN', icon: Icons.assignment_outlined),
      AppNavigationItem(label: 'Receiving', icon: Icons.move_to_inbox_outlined),
      AppNavigationItem(label: 'Put away', icon: Icons.inventory_2_outlined),
    ],
  ),
  AppNavigationGroup(
    label: 'Outbound',
    items: [
      AppNavigationItem(
        label: 'Sales order',
        icon: Icons.receipt_long_outlined,
      ),
      AppNavigationItem(label: 'Picking', icon: Icons.checklist_outlined),
      AppNavigationItem(label: 'Packing', icon: Icons.inventory_outlined),
      AppNavigationItem(label: 'Shipping', icon: Icons.local_shipping_outlined),
    ],
  ),
  AppNavigationGroup(
    label: 'Operations',
    items: [
      AppNavigationItem(
        label: 'Inventory',
        icon: Icons.analytics_outlined,
        location: '/inventory',
      ),
      AppNavigationItem(
        label: 'Inventory ledger',
        icon: Icons.receipt_long_outlined,
        location: '/inventory-ledger',
      ),
      AppNavigationItem(label: 'Reports', icon: Icons.bar_chart_outlined),
      AppNavigationItem(label: 'Settings', icon: Icons.settings_outlined),
    ],
  ),
];
