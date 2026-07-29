import 'package:flutter/material.dart';

import '../../../../shared/layout/page_layout.dart';

class MasterDetailPage extends StatelessWidget {
  const MasterDetailPage({
    super.key,
    required this.title,
    required this.general,
    required this.audit,
    this.status,
    this.actions = const [],
  });
  final String title;
  final List<Widget> general;
  final List<Widget> audit;
  final Widget? status;
  final List<Widget> actions;
  @override
  Widget build(BuildContext context) => AppPage(
    title: title,
    actions: actions,
    body: ListView(
      children: [
        ?status,
        const ListTile(title: Text('General information')),
        ...general,
        const Divider(),
        const ListTile(title: Text('Audit information')),
        ...audit,
      ],
    ),
  );
}
