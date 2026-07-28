import 'package:flutter/material.dart';

import '../../core/constants/app_spacing.dart';
import 'responsive_layout.dart';

class ContentLayout extends StatelessWidget {
  const ContentLayout({super.key, required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) => Align(
      alignment: Alignment.topCenter,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: AppSpacing.xxxl * 24),
        child: SizedBox(
          height: constraints.maxHeight,
          child: Padding(
            padding: ResponsiveLayout.pagePaddingOf(context),
            child: child,
          ),
        ),
      ),
    ),
  );
}
