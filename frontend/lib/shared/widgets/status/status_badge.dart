import 'package:flutter/material.dart';

enum AppStatus {
  active,
  inactive,
  pending,
  approved,
  rejected,
  draft,
  cancelled,
  completed,
  closed,
  reserved,
  available,
  lowStock,
  outOfStock,
}

class AppStatusBadge extends StatelessWidget {
  const AppStatusBadge({super.key, required this.status, this.label});
  final AppStatus status;
  final String? label;
  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final color = switch (status) {
      AppStatus.rejected ||
      AppStatus.cancelled ||
      AppStatus.outOfStock => colors.error,
      AppStatus.pending ||
      AppStatus.lowStock ||
      AppStatus.reserved => colors.tertiary,
      AppStatus.active ||
      AppStatus.approved ||
      AppStatus.completed ||
      AppStatus.available => colors.primary,
      _ => colors.secondary,
    };
    return Chip(
      avatar: Icon(Icons.circle, size: 8, color: color),
      label: Text(label ?? _label),
      side: BorderSide.none,
      visualDensity: VisualDensity.compact,
    );
  }

  String get _label => switch (status) {
    AppStatus.lowStock => 'Low stock',
    AppStatus.outOfStock => 'Out of stock',
    _ => status.name[0].toUpperCase() + status.name.substring(1),
  };
}
