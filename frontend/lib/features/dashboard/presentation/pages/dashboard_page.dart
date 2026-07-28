import 'package:flutter/material.dart';

import '../../../../core/constants/app_spacing.dart';
import '../../../../core/constants/app_radius.dart';
import '../../../../shared/widgets/app_components.dart';

class DashboardPage extends StatelessWidget {
  const DashboardPage({super.key});
  @override
  Widget build(BuildContext context) => LayoutBuilder(builder: (context, constraints) {
    final columns = constraints.maxWidth > 1100 ? 4 : constraints.maxWidth > 700 ? 2 : 1;
    return ListView(padding: AppSpacing.page, children: [
      Text('Warehouse operations at a glance', style: Theme.of(context).textTheme.bodyLarge),
      const SizedBox(height: AppSpacing.lg),
      GridView.count(crossAxisCount: columns, shrinkWrap: true, physics: const NeverScrollableScrollPhysics(), crossAxisSpacing: AppSpacing.md, mainAxisSpacing: AppSpacing.md, childAspectRatio: 2.05, children: const [
        _KpiCard(label: 'Total inventory', value: '128,430', icon: Icons.inventory_2_outlined),
        _KpiCard(label: 'Low stock items', value: '24', icon: Icons.warning_amber_outlined, tone: StatusTone.warning),
        _KpiCard(label: 'Pending tasks', value: '18', icon: Icons.assignment_outlined),
        _KpiCard(label: 'Fulfillment rate', value: '98.4%', icon: Icons.trending_up_outlined, tone: StatusTone.success),
      ]),
      const SizedBox(height: AppSpacing.lg),
      Wrap(spacing: AppSpacing.md, runSpacing: AppSpacing.md, children: [
        SizedBox(width: constraints.maxWidth > 900 ? (constraints.maxWidth - AppSpacing.md) * .62 : constraints.maxWidth, child: const _InventorySummary()),
        SizedBox(width: constraints.maxWidth > 900 ? (constraints.maxWidth - AppSpacing.md) * .38 : constraints.maxWidth, child: const _QuickActions()),
      ]),
      const SizedBox(height: AppSpacing.lg),
      const _ActivityTable(),
    ]);
  });
}

class _KpiCard extends StatelessWidget {
  const _KpiCard({required this.label, required this.value, required this.icon, this.tone = StatusTone.neutral});
  final String label, value; final IconData icon; final StatusTone tone;
  @override Widget build(BuildContext context) => AppSectionCard(title: label, child: Row(children: [Expanded(child: Text(value, style: Theme.of(context).textTheme.headlineMedium)), Icon(icon, color: Theme.of(context).colorScheme.primary), const SizedBox(width: AppSpacing.xs), StatusBadge(label: tone == StatusTone.warning ? 'Action' : 'Live', tone: tone)]));
}

class _InventorySummary extends StatelessWidget {
  const _InventorySummary();
  @override Widget build(BuildContext context) => AppSectionCard(title: 'Inventory summary', action: TextButton(onPressed: () {}, child: const Text('View report')), child: const SizedBox(height: 180, child: _SummaryBars()));
}
class _SummaryBars extends StatelessWidget {
  const _SummaryBars();
  @override Widget build(BuildContext context) => Row(crossAxisAlignment: CrossAxisAlignment.end, mainAxisAlignment: MainAxisAlignment.spaceEvenly, children: const [
    _Bar(label: 'Mon', height: 72), _Bar(label: 'Tue', height: 120), _Bar(label: 'Wed', height: 96), _Bar(label: 'Thu', height: 148), _Bar(label: 'Fri', height: 116), _Bar(label: 'Sat', height: 88),
  ]);
}
class _Bar extends StatelessWidget { const _Bar({required this.label, required this.height}); final String label; final double height;
  @override Widget build(BuildContext context) => Column(mainAxisAlignment: MainAxisAlignment.end, children: [Container(width: 24, height: height, decoration: BoxDecoration(color: Theme.of(context).colorScheme.primary, borderRadius: BorderRadius.circular(AppRadius.input))), const SizedBox(height: AppSpacing.xs), Text(label)]);
}
class _QuickActions extends StatelessWidget { const _QuickActions(); @override Widget build(BuildContext context) => AppSectionCard(title: 'Quick actions', child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [FilledButton.icon(onPressed: () {}, icon: const Icon(Icons.add), label: const Text('Create inbound receipt')), const SizedBox(height: AppSpacing.sm), OutlinedButton.icon(onPressed: () {}, icon: const Icon(Icons.playlist_add_check), label: const Text('Review pending tasks')), const SizedBox(height: AppSpacing.sm), TextButton.icon(onPressed: () {}, icon: const Icon(Icons.qr_code_scanner), label: const Text('Start stock count'))])); }
class _ActivityTable extends StatelessWidget { const _ActivityTable(); @override Widget build(BuildContext context) => AppSectionCard(title: 'Recent activity', action: const AppSearchField(hintText: 'Search activity'), child: SingleChildScrollView(scrollDirection: Axis.horizontal, child: DataTable(columns: const [DataColumn(label: Text('Reference')), DataColumn(label: Text('Activity')), DataColumn(label: Text('Warehouse')), DataColumn(label: Text('Status')), DataColumn(label: Text('Updated'))], rows: const [DataRow(cells: [DataCell(Text('GRN-20481')), DataCell(Text('Goods receipt')), DataCell(Text('Jakarta DC')), DataCell(StatusBadge(label: 'Completed', tone: StatusTone.success)), DataCell(Text('Just now'))]), DataRow(cells: [DataCell(Text('SO-10823')), DataCell(Text('Pick task')), DataCell(Text('Bekasi DC')), DataCell(StatusBadge(label: 'In progress', tone: StatusTone.warning)), DataCell(Text('12 min ago'))]), DataRow(cells: [DataCell(Text('ADJ-0082')), DataCell(Text('Stock adjustment')), DataCell(Text('Jakarta DC')), DataCell(StatusBadge(label: 'Completed', tone: StatusTone.success)), DataCell(Text('1 hr ago'))])]))); }
