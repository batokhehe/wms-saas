import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

class AppNumberField extends StatelessWidget {
  const AppNumberField({
    super.key,
    this.controller,
    this.label,
    this.onChanged,
    this.validator,
    this.enabled = true,
    this.readOnly = false,
  });
  final TextEditingController? controller;
  final String? label;
  final ValueChanged<String>? onChanged;
  final FormFieldValidator<String>? validator;
  final bool enabled, readOnly;
  @override
  Widget build(BuildContext context) => TextFormField(
    controller: controller,
    keyboardType: const TextInputType.numberWithOptions(decimal: true),
    inputFormatters: [FilteringTextInputFormatter.allow(RegExp(r'^\d*\.?\d*'))],
    onChanged: onChanged,
    validator: validator,
    enabled: enabled,
    readOnly: readOnly,
    decoration: InputDecoration(labelText: label),
  );
}
