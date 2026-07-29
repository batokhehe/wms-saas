import 'package:flutter/material.dart';

import '../../../../shared/widgets/status/status_badge.dart';

class StatusChip extends StatelessWidget {
  const StatusChip({super.key, required this.status});
  final AppStatus status;
  @override
  Widget build(BuildContext context) => AppStatusBadge(status: status);
}
