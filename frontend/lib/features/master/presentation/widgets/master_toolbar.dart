import 'package:flutter/material.dart';

import '../../../../shared/widgets/buttons/app_button.dart';

class MasterToolbar extends StatelessWidget {
  const MasterToolbar({
    super.key,
    this.createLabel,
    this.onCreate,
    this.trailing = const [],
  });
  final String? createLabel;
  final VoidCallback? onCreate;
  final List<Widget> trailing;
  @override
  Widget build(BuildContext context) => Wrap(
    spacing: 8,
    children: [
      if (createLabel != null)
        AppButton(label: createLabel!, icon: Icons.add, onPressed: onCreate),
      ...trailing,
    ],
  );
}
