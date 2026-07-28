import 'package:flutter/material.dart';

class LoadingOverlay extends StatelessWidget {
  const LoadingOverlay({super.key, required this.child, this.loading = false});
  final Widget child;
  final bool loading;
  @override
  Widget build(BuildContext context) => Stack(
    children: [
      child,
      if (loading)
        Positioned.fill(
          child: ColoredBox(
            color: Theme.of(context).colorScheme.scrim.withValues(alpha: 0.16),
            child: const Center(child: CircularProgressIndicator()),
          ),
        ),
    ],
  );
}
