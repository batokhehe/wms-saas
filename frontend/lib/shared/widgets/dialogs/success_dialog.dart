import 'package:flutter/material.dart';

import '../../../core/constants/app_spacing.dart';
import 'app_dialog.dart';

class SuccessDialog extends StatelessWidget {
  const SuccessDialog({super.key, required this.message, this.onClose});
  final String message;
  final VoidCallback? onClose;
  @override
  Widget build(BuildContext context) => AppDialog(
    title: 'Success',
    content: Row(
      children: [
        Icon(
          Icons.check_circle_outline,
          color: Theme.of(context).colorScheme.primary,
        ),
        const SizedBox(width: AppSpacing.xs),
        Expanded(child: Text(message)),
      ],
    ),
    actions: [
      FilledButton(
        onPressed: onClose ?? () => Navigator.pop(context),
        child: const Text('Done'),
      ),
    ],
  );
}
