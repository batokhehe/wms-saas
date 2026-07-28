import 'package:flutter/material.dart';

import 'responsive_layout.dart';

class AppLayout extends StatelessWidget {
  const AppLayout({
    super.key,
    required this.topBar,
    required this.sidebar,
    required this.child,
  });
  final PreferredSizeWidget topBar;
  final Widget sidebar;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final viewport = ResponsiveLayout.viewportOf(context);
    return Scaffold(
      appBar: topBar,
      drawer: viewport == AppViewport.mobile
          ? Drawer(child: SafeArea(child: sidebar))
          : null,
      body: Row(
        children: [
          if (viewport != AppViewport.mobile) sidebar,
          if (viewport != AppViewport.mobile) const VerticalDivider(width: 1),
          Expanded(child: child),
        ],
      ),
    );
  }
}
