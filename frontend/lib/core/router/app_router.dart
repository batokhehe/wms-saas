import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../features/dashboard/presentation/pages/dashboard_page.dart';
import '../../features/warehouse/warehouse.dart';
import '../../features/location/location.dart';
import '../../features/product/product.dart';
import '../../features/uom/uom.dart';
import '../../features/category/category.dart';
import '../../features/brand/brand.dart';
import '../../features/supplier/supplier.dart';
import '../../features/customer/customer.dart';
import '../../features/inventory/inventory.dart';
import '../../features/inventory_ledger/inventory_ledger.dart';
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
    if (signedIn && (path == '/login' || path == '/splash')) {
      return '/dashboard';
    }
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
          path: '/warehouses',
          builder: (context, state) => const WarehouseListPage(),
        ),
        GoRoute(
          path: '/warehouses/create',
          builder: (context, state) => const WarehouseFormPage(),
        ),
        GoRoute(
          path: '/warehouses/:id',
          builder: (context, state) =>
              WarehouseDetailPage(id: state.pathParameters['id']!),
        ),
        GoRoute(
          path: '/warehouses/:id/edit',
          redirect: (context, state) =>
              '/warehouses/${state.pathParameters['id']}',
        ),
        GoRoute(
          path: '/products',
          builder: (context, state) => const ProductListPage(),
        ),
        GoRoute(
          path: '/uoms',
          builder: (context, state) => const UOMListPage(),
        ),
        GoRoute(
          path: '/categories',
          builder: (context, state) => const CategoryListPage(),
        ),
        GoRoute(
          path: '/brands',
          builder: (context, state) => const BrandListPage(),
        ),
        GoRoute(
          path: '/locations',
          builder: (context, state) => const LocationListPage(),
        ),
        GoRoute(
          path: '/locations/create',
          builder: (context, state) => const LocationFormPage(),
        ),
        GoRoute(
          path: '/locations/:id',
          builder: (context, state) =>
              LocationDetailPage(id: state.pathParameters['id']!),
        ),
        GoRoute(
          path: '/locations/:id/edit',
          redirect: (context, state) =>
              '/locations/${state.pathParameters['id']}',
        ),
        GoRoute(
          path: '/suppliers',
          builder: (context, state) => const SupplierListPage(),
        ),
        GoRoute(
          path: '/suppliers/create',
          builder: (context, state) => const SupplierFormPage(),
        ),
        GoRoute(
          path: '/suppliers/:id',
          builder: (context, state) =>
              SupplierDetailPage(id: state.pathParameters['id']!),
        ),
        GoRoute(
          path: '/suppliers/:id/edit',
          redirect: (context, state) =>
              '/suppliers/${state.pathParameters['id']}',
        ),
        GoRoute(
          path: '/customers',
          builder: (context, state) => const CustomerListPage(),
        ),
        GoRoute(
          path: '/customers/create',
          builder: (context, state) => const CustomerFormPage(),
        ),
        GoRoute(
          path: '/customers/:id',
          builder: (context, state) =>
              CustomerDetailPage(id: state.pathParameters['id']!),
        ),
        GoRoute(
          path: '/customers/:id/edit',
          redirect: (context, state) =>
              '/customers/${state.pathParameters['id']}',
        ),
        GoRoute(
          path: '/inventory',
          builder: (context, state) => const InventoryListPage(),
        ),
        // Declared before /inventory/:id so "receive" is matched as a literal
        // segment rather than being captured as a position id.
        GoRoute(
          path: '/inventory/receive',
          builder: (context, state) => const InventoryReceivePage(),
        ),
        GoRoute(
          path: '/inventory/:id',
          builder: (context, state) =>
              InventoryDetailPage(id: state.pathParameters['id']!),
        ),
        GoRoute(
          path: '/inventory-ledger',
          builder: (context, state) => InventoryLedgerListPage(
            positionId: state.uri.queryParameters['position_id'],
          ),
        ),
        GoRoute(
          path: '/inventory-ledger/:id',
          builder: (context, state) =>
              InventoryLedgerDetailPage(id: state.pathParameters['id']!),
        ),
        GoRoute(
          path: '/design-system',
          builder: (context, state) => const DesignSystemPage(),
        ),
      ],
    ),
  ],
);
