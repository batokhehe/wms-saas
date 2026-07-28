import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../features/dashboard/presentation/pages/dashboard_page.dart';
import '../../features/auth/presentation/pages/auth_pages.dart';
import '../../features/auth/domain/entities/current_user.dart';
import '../../shared/pages/design_system_page.dart';
import '../../shared/widgets/app_shell.dart';

GoRouter createAppRouter(AsyncValue<CurrentUser?> auth) => GoRouter(
  initialLocation: '/splash',
  redirect: (context, state) {
    final path = state.uri.path;
    if (auth.isLoading) return path == '/splash' ? null : '/splash';
    final signedIn = auth.hasValue && auth.value != null;
    final public =
        path == '/login' ||
        path == '/forgot-password' ||
        path == '/unauthorized';
    if (!signedIn && !public) return '/login';
    if (signedIn && (path == '/login' || path == '/splash'))
      return '/dashboard';
    return null;
  },
  routes: [
    GoRoute(path: '/splash', builder: (context, state) => const SplashPage()),
    GoRoute(path: '/login', builder: (context, state) => const LoginPage()),
    GoRoute(
      path: '/forgot-password',
      builder: (context, state) => const ForgotPasswordPage(),
    ),
    GoRoute(
      path: '/unauthorized',
      builder: (context, state) => const UnauthorizedPage(),
    ),
    ShellRoute(
      builder: (context, state, child) => AppShell(child: child),
      routes: [
        GoRoute(
          path: '/dashboard',
          builder: (context, state) => const DashboardPage(),
        ),
        GoRoute(
          path: '/design-system',
          builder: (context, state) => const DesignSystemPage(),
        ),
      ],
    ),
  ],
);
