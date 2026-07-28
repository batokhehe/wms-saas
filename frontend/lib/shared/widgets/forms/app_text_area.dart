import 'package:flutter/material.dart';

import 'app_text_field.dart';

class AppTextArea extends StatelessWidget {
  const AppTextArea({
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
  Widget build(BuildContext context) => AppTextField(
    controller: controller,
    label: label,
    onChanged: onChanged,
    validator: validator,
    enabled: enabled,
    readOnly: readOnly,
    maxLines: 4,
  );
}
