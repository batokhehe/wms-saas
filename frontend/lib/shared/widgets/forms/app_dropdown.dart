import 'package:flutter/material.dart';

class AppDropdown<T> extends StatelessWidget {
  const AppDropdown({
    super.key,
    required this.items,
    this.value,
    this.onChanged,
    this.label,
    this.hintText,
    this.enabled = true,
  });
  final List<DropdownMenuItem<T>> items;
  final T? value;
  final ValueChanged<T?>? onChanged;
  final String? label, hintText;
  final bool enabled;
  @override
  Widget build(BuildContext context) => DropdownButtonFormField<T>(
    items: items,
    initialValue: value,
    onChanged: enabled ? onChanged : null,
    decoration: InputDecoration(labelText: label, hintText: hintText),
  );
}
