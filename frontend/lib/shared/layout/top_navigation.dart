import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/constants/app_spacing.dart';
import '../controllers/theme_controller.dart';
import 'responsive_layout.dart';

class AppTopNavigation extends ConsumerWidget implements PreferredSizeWidget {
  const AppTopNavigation({super.key, required this.breadcrumb});
  final Widget breadcrumb;

  @override
  Size get preferredSize => const Size.fromHeight(kToolbarHeight);

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final isMobile = ResponsiveLayout.viewportOf(context) == AppViewport.mobile;
    return AppBar(
      titleSpacing: AppSpacing.lg,
      title: breadcrumb,
      actions: [
        if (!isMobile) const _TopBarSearch(),
        IconButton(
          tooltip: 'Notifications',
          onPressed: () => _showPlaceholder(context, 'Notifications'),
          icon: const Badge(child: Icon(Icons.notifications_none)),
        ),
        IconButton(
          tooltip: 'Toggle color theme',
          onPressed: ref.read(themeModeProvider.notifier).toggle,
          icon: const Icon(Icons.dark_mode_outlined),
        ),
        IconButton(
          tooltip: 'Change language',
          onPressed: () => _showPlaceholder(context, 'Language selector'),
          icon: const Icon(Icons.language),
        ),
        if (!isMobile)
          TextButton.icon(
            onPressed: () => _showPlaceholder(context, 'Company selector'),
            icon: const Icon(Icons.business_outlined),
            label: const Text('Acme WMS'),
          ),
        PopupMenuButton<String>(
          tooltip: 'User menu',
          onSelected: (value) => _showPlaceholder(
            context,
            value == 'logout' ? 'Logout' : 'Profile',
          ),
          itemBuilder: (context) => const [
            PopupMenuItem(value: 'profile', child: Text('Profile')),
            PopupMenuDivider(),
            PopupMenuItem(value: 'logout', child: Text('Logout')),
          ],
          child: const Padding(
            padding: EdgeInsets.only(right: AppSpacing.md),
            child: CircleAvatar(radius: AppSpacing.md, child: Text('NM')),
          ),
        ),
      ],
    );
  }
}

class _TopBarSearch extends StatelessWidget {
  const _TopBarSearch();
  @override
  Widget build(BuildContext context) => ConstrainedBox(
    constraints: const BoxConstraints(maxWidth: AppSpacing.xxxl * 5),
    child: TextField(
      readOnly: true,
      onTap: () => _showPlaceholder(context, 'Global search'),
      decoration: const InputDecoration(
        isDense: true,
        hintText: 'Search',
        prefixIcon: Icon(Icons.search),
      ),
    ),
  );
}

void _showPlaceholder(BuildContext context, String label) {
  ScaffoldMessenger.of(
    context,
  ).showSnackBar(SnackBar(content: Text('$label is coming soon.')));
}
