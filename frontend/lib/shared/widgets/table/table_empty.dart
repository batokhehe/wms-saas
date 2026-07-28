import 'package:flutter/material.dart';

import '../feedback/empty_state.dart';

class TableEmpty extends StatelessWidget {
  const TableEmpty({
    super.key,
    this.title = 'No records found',
    this.description = 'Try changing your search or filters.',
  });
  final String title, description;
  @override
  Widget build(BuildContext context) =>
      AppEmptyState(title: title, description: description);
}
