import 'package:flutter/material.dart';

import '../../../../shared/widgets/dialogs/confirmation_dialog.dart';

class LifecycleDialog extends StatelessWidget {
  const LifecycleDialog({
    super.key,
    required this.itemName,
    required this.activate,
    required this.onConfirm,
  });
  final String itemName;
  final bool activate;
  final VoidCallback onConfirm;
  @override
  Widget build(BuildContext context) => ConfirmationDialog(
    title: '${activate ? 'Activate' : 'Deactivate'} $itemName?',
    message: 'Confirm this lifecycle action.',
    confirmLabel: activate ? 'Activate' : 'Deactivate',
    onConfirm: onConfirm,
  );
}
