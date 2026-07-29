import 'package:flutter/material.dart';

import '../../../../shared/widgets/dialogs/delete_dialog.dart';

class MasterDeleteDialog extends StatelessWidget {
  const MasterDeleteDialog({
    super.key,
    required this.itemName,
    required this.onDelete,
  });
  final String itemName;
  final VoidCallback onDelete;
  @override
  Widget build(BuildContext context) =>
      DeleteDialog(itemName: itemName, onDelete: onDelete);
}
