import 'package:flutter/material.dart';

class AppTextField extends StatelessWidget {
  const AppTextField({
    super.key,
    this.controller,
    this.label,
    this.hintText,
    this.helperText,
    this.errorText,
    this.prefixIcon,
    this.suffixIcon,
    this.onChanged,
    this.validator,
    this.enabled = true,
    this.readOnly = false,
    this.loading = false,
    this.obscureText = false,
    this.maxLines = 1,
  });
  final TextEditingController? controller;
  final String? label, hintText, helperText, errorText;
  final Widget? prefixIcon, suffixIcon;
  final ValueChanged<String>? onChanged;
  final FormFieldValidator<String>? validator;
  final bool enabled, readOnly, loading, obscureText;
  final int maxLines;
  @override
  Widget build(BuildContext context) => TextFormField(
    controller: controller,
    onChanged: onChanged,
    validator: validator,
    enabled: enabled && !loading,
    readOnly: readOnly || loading,
    obscureText: obscureText,
    maxLines: maxLines,
    decoration: InputDecoration(
      labelText: label,
      hintText: hintText,
      helperText: helperText,
      errorText: errorText,
      prefixIcon: prefixIcon,
      suffixIcon: loading
          ? const Padding(
              padding: EdgeInsets.all(12),
              child: SizedBox.square(
                dimension: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              ),
            )
          : suffixIcon,
    ),
  );
}
