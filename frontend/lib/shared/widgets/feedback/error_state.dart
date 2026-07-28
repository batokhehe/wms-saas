import 'package:flutter/material.dart';

import '../buttons/app_button.dart';
import 'empty_state.dart';

class AppErrorState extends StatelessWidget {
  const AppErrorState({super.key, required this.message, this.onRetry});
  final String message;
  final VoidCallback? onRetry;
  @override
  Widget build(BuildContext context) => AppEmptyState(
    title: 'Something went wrong',
    description: message,
    icon: Icons.error_outline,
    action: onRetry == null
        ? null
        : AppButton(label: 'Retry', icon: Icons.refresh, onPressed: onRetry),
  );
}
