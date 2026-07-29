import 'package:flutter/material.dart';

import '../../../../shared/layout/page_layout.dart';

class MasterFormPage extends StatelessWidget {
  const MasterFormPage({
    super.key,
    required this.title,
    required this.child,
    this.loading = false,
  });
  final String title;
  final Widget child;
  final bool loading;
  @override
  Widget build(BuildContext context) => AppPage(
    title: title,
    loading: loading,
    body: ListView(children: [child]),
  );
}
