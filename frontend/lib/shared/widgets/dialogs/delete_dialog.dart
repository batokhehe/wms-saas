import 'package:flutter/material.dart';

import 'confirmation_dialog.dart';

class DeleteDialog extends StatelessWidget {
  const DeleteDialog({
    super.key,
    required this.itemName,
    required this.onDelete,
  });
  final String itemName;
  final VoidCallback onDelete;
  @override
  Widget build(BuildContext context) => ConfirmationDialog(
    title: 'Delete $itemName?',
    message: 'This action cannot be undone.',
    confirmLabel: 'Delete',
    onConfirm: onDelete,
  );
}
