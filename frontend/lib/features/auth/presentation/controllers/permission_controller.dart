import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../../shared/widgets/feedback/empty_state.dart';
import '../../../../shared/widgets/feedback/error_state.dart';
import '../../../../shared/widgets/feedback/page_loading.dart';
import 'auth_controller.dart';

/// The permission codes the signed-in user holds in the active company.
///
/// Sourced from `GET /permissions/mine`, which the backend deliberately leaves
/// unguarded: a client cannot render a usable interface without knowing what it
/// may do, and requiring `permission.read` to discover whether you hold
/// `permission.read` is circular.
///
/// This is a presentation convenience, never a security boundary. Every route is
/// enforced again on the server, so a client that ignored this entirely would
/// gain nothing.
final myPermissionsProvider = FutureProvider<Set<String>>((ref) async {
  final user = await ref.watch(currentUserProvider.future);
  if (user == null) return const <String>{};
  final response = await ref
      .read(apiClientProvider)
      .dio
      .get('/permissions/mine');
  final data = response.data['data'] as Map<String, dynamic>;
  final codes = data['permissions'] as List<dynamic>? ?? const [];
  return codes.map((code) => code as String).toSet();
});

extension PermissionRef on WidgetRef {
  /// Whether the user holds [permission].
  ///
  /// An unresolved permission set reads as false, so a control stays hidden
  /// until its grant is known rather than appearing and then disappearing.
  bool can(String permission) =>
      watch(myPermissionsProvider).value?.contains(permission) ?? false;
}

/// Renders [child] only when the user holds [permission], and an access notice
/// otherwise. Use it to guard a whole page.
class PermissionGuard extends ConsumerWidget {
  const PermissionGuard({
    super.key,
    required this.permission,
    required this.child,
  });
  final String permission;
  final Widget child;
  @override
  Widget build(BuildContext context, WidgetRef ref) =>
      ref
          .watch(myPermissionsProvider)
          .when(
            loading: () => const PageLoading(),
            error: (error, stack) => AppErrorState(
              message: '$error',
              onRetry: () => ref.invalidate(myPermissionsProvider),
            ),
            data: (granted) => granted.contains(permission)
                ? child
                : const AppEmptyState(
                    title: 'You do not have access',
                    description:
                        'Ask an administrator to grant this permission.',
                    icon: Icons.lock_outline,
                  ),
          );
}

/// Shows [child] when the user holds [permission] and nothing otherwise. Use it
/// to hide an action a user cannot perform.
class PermissionGate extends ConsumerWidget {
  const PermissionGate({
    super.key,
    required this.permission,
    required this.child,
  });
  final String permission;
  final Widget child;
  @override
  Widget build(BuildContext context, WidgetRef ref) =>
      ref.can(permission) ? child : const SizedBox.shrink();
}
