import 'package:flutter/material.dart';

class AppButton extends StatelessWidget {
  const AppButton({
    super.key,
    required this.label,
    this.onPressed,
    this.icon,
    this.loading = false,
    this.isOutlined = false,
  });
  final String label;
  final VoidCallback? onPressed;
  final IconData? icon;
  final bool loading;
  final bool isOutlined;
  @override
  Widget build(BuildContext context) {
    final child = loading
        ? const SizedBox.square(
            dimension: 18,
            child: CircularProgressIndicator(strokeWidth: 2),
          )
        : Text(label);
    if (isOutlined) {
      return icon == null
          ? OutlinedButton(onPressed: loading ? null : onPressed, child: child)
          : OutlinedButton.icon(
              onPressed: loading ? null : onPressed,
              icon: Icon(icon),
              label: child,
            );
    }
    return icon == null
        ? FilledButton(onPressed: loading ? null : onPressed, child: child)
        : FilledButton.icon(
            onPressed: loading ? null : onPressed,
            icon: Icon(icon),
            label: child,
          );
  }
}
