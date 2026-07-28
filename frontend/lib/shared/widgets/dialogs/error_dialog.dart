import 'package:flutter/material.dart';

import 'app_dialog.dart';

class ErrorDialog extends StatelessWidget {
  const ErrorDialog({super.key, required this.message, this.onClose});
  final String message;
  final VoidCallback? onClose;
  @override
  Widget build(BuildContext context) => AppDialog(
    title: 'Unable to complete action',
    content: Text(message),
    actions: [
      FilledButton(
        onPressed: onClose ?? () => Navigator.pop(context),
        child: const Text('Close'),
      ),
    ],
  );
}
