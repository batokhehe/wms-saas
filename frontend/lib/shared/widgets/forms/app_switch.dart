import 'package:flutter/material.dart';

class AppSwitch extends StatelessWidget {
  const AppSwitch({
    super.key,
    required this.label,
    required this.value,
    this.onChanged,
    this.enabled = true,
  });
  final String label;
  final bool value, enabled;
  final ValueChanged<bool>? onChanged;
  @override
  Widget build(BuildContext context) => SwitchListTile.adaptive(
    title: Text(label),
    value: value,
    onChanged: enabled ? onChanged : null,
  );
}
