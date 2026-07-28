import 'package:flutter/material.dart';

import 'app_card.dart';

class ChartCard extends StatelessWidget {
  const ChartCard({
    super.key,
    required this.title,
    required this.child,
    this.action,
  });
  final String title;
  final Widget child;
  final Widget? action;
  @override
  Widget build(BuildContext context) =>
      AppCard(title: title, action: action, child: child);
}
