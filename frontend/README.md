# WMS SaaS frontend

Flutter Web design-system foundation for the WMS SaaS. This sprint deliberately includes no APIs, repositories, or business modules.

## Structure

`lib/core` holds application-wide routing, theme, and design tokens. `lib/shared` contains reusable presentation components and shell state. `lib/features/dashboard` is a presentation-only dashboard feature; future features follow the same `presentation/domain/data` boundary.

## Architecture

Presentation widgets compose shared components and consume only presentation state. Future domain abstractions are introduced beneath feature presentation; data implementations sit below domain. This keeps widgets independent of HTTP and persistence details.

## Included components

Material 3 light/dark themes, GoRouter shell navigation, responsive navigation rail/drawer, KPI cards, status badges, search input, action buttons, dialogs-ready theme configuration, responsive data table, loading skeleton, empty and error states.

## Responsive behavior

The layout is desktop-first. The persistent navigation rail becomes a drawer below 760px; dashboard KPI and content grids reduce their column count from four to two to one based on available width.

## Run

```bash
flutter pub get
flutter run -d chrome
flutter analyze
```
