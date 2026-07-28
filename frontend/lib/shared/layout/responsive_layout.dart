import 'package:flutter/widgets.dart';

import '../../core/constants/app_spacing.dart';

enum AppViewport { mobile, tablet, desktop }

abstract final class ResponsiveLayout {
  static AppViewport viewportOf(BuildContext context) {
    final width = MediaQuery.sizeOf(context).width;
    if (width < AppSpacing.xxxl * 12) {
      return AppViewport.mobile;
    }
    if (width < AppSpacing.xxxl * 18) {
      return AppViewport.tablet;
    }
    return AppViewport.desktop;
  }

  static EdgeInsets pagePaddingOf(BuildContext context) =>
      switch (viewportOf(context)) {
        AppViewport.mobile => const EdgeInsets.all(AppSpacing.md),
        AppViewport.tablet => const EdgeInsets.all(AppSpacing.lg),
        AppViewport.desktop => const EdgeInsets.all(AppSpacing.xl),
      };
}
