# WMS SaaS frontend

Flutter Web foundation for the WMS SaaS. The current implementation is limited to the design system and enterprise application framework; it contains no API integration, repositories, or business-module implementation.

## Architecture

`core/` owns cross-cutting configuration, design tokens, theme, and the frozen GoRouter configuration. `shared/` owns reusable presentation infrastructure. Feature pages are composed as `AppShell → AppPage → feature widgets`; future feature domains and data layers remain below presentation.

## Application framework

`shared/layout/` contains the reusable shell, responsive layout, top navigation, sidebar, breadcrumbs, content constraint, and `AppPage`. The `FutureRoutes` contract records intended module paths without changing the current router or activating placeholder business pages.

```text
AppShell
├── AppTopNavigation (breadcrumb, search, notification, theme, language, company, user menu)
└── AppLayout
    ├── AppSidebar
    └── AppPage
        └── Feature content
```

## Responsive behavior

```text
Desktop  → permanent expanded/collapsible sidebar
Tablet   → permanent compact navigation rail
Mobile   → sidebar placed in the Scaffold drawer
```

Page padding and content width are supplied by `ResponsiveLayout` and `ContentLayout`; no feature needs to duplicate a viewport-specific page structure.

## Enterprise component library

The shared widget library now provides Material 3 wrappers for buttons, cards, forms, dialogs, feedback states, filtering, search, statuses, charts, and enterprise data tables. Components are presentation-only and receive callbacks/data from future feature presentation layers.

```text
shared/widgets/
├── buttons/   cards/    charts/   dialogs/
├── feedback/  filters/  forms/    search/
├── status/    table/
└── app_components.dart  (legacy compatibility)
```

`AppDataTable` includes paging, column visibility, selection and bulk-action hooks, loading/empty/error states, search/filter hooks, external sort callbacks, responsive scrolling, and explicit export/column-resize placeholders. It does not own data fetching or sorting logic.

## Run

```bash
dart format .
flutter analyze
flutter test
flutter run -d chrome
```
