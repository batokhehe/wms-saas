import 'package:flutter/material.dart';

import 'app_dialog.dart';

class ConfirmationDialog extends StatelessWidget {
  const ConfirmationDialog({
    super.key,
    required this.title,
    required this.message,
    required this.onConfirm,
    this.confirmLabel = 'Confirm',
  });
  final String title, message, confirmLabel;
  final VoidCallback onConfirm;
  @override
  Widget build(BuildContext context) => AppDialog(
    title: title,
    content: Text(message),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Cancel'),
      ),
      FilledButton(onPressed: onConfirm, child: Text(confirmLabel)),
    ],
  );
}
