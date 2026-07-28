import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';

class AppDialog extends StatelessWidget {
  const AppDialog({
    super.key,
    required this.title,
    required this.content,
    this.actions = const [],
  });
  final String title;
  final Widget content;
  final List<Widget> actions;
  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(title),
    content: ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: AppSpacing.xxxl * 8),
      child: content,
    ),
    actions: actions,
  );
}

Future<T?> showAppDialog<T>(BuildContext context, AppDialog dialog) =>
    showDialog<T>(context: context, builder: (context) => dialog);
