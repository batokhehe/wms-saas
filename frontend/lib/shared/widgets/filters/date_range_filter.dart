import 'package:flutter/material.dart';

class DateRangeFilter extends StatelessWidget {
  const DateRangeFilter({super.key, this.value, this.onChanged});
  final DateTimeRange? value;
  final ValueChanged<DateTimeRange?>? onChanged;
  @override
  Widget build(BuildContext context) => OutlinedButton.icon(
    onPressed: () async {
      final range = await showDateRangePicker(
        context: context,
        firstDate: DateTime(DateTime.now().year - 100),
        lastDate: DateTime(DateTime.now().year + 100),
        initialDateRange: value,
      );
      onChanged?.call(range);
    },
    icon: const Icon(Icons.date_range_outlined),
    label: Text(
      value == null
          ? 'Date range'
          : '${MaterialLocalizations.of(context).formatShortDate(value!.start)} – ${MaterialLocalizations.of(context).formatShortDate(value!.end)}',
    ),
  );
}
