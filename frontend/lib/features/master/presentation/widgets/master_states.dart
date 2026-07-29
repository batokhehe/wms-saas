import 'package:flutter/material.dart';

import '../../../../shared/widgets/feedback/empty_state.dart';
import '../../../../shared/widgets/feedback/error_state.dart';
import '../../../../shared/widgets/feedback/page_loading.dart';

class MasterLoading extends StatelessWidget {
  const MasterLoading({super.key});
  @override
  Widget build(BuildContext context) => const PageLoading();
}

class MasterEmptyState extends StatelessWidget {
  const MasterEmptyState({
    super.key,
    required this.title,
    required this.description,
  });
  final String title, description;
  @override
  Widget build(BuildContext context) =>
      AppEmptyState(title: title, description: description);
}

class MasterErrorState extends StatelessWidget {
  const MasterErrorState({super.key, required this.message, this.onRetry});
  final String message;
  final VoidCallback? onRetry;
  @override
  Widget build(BuildContext context) =>
      AppErrorState(message: message, onRetry: onRetry);
}
