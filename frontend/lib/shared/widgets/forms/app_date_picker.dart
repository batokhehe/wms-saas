import 'package:flutter/material.dart';

class AppDatePicker extends StatelessWidget {
  const AppDatePicker({
    super.key,
    required this.label,
    this.value,
    this.onChanged,
    this.enabled = true,
  });
  final String label;
  final DateTime? value;
  final ValueChanged<DateTime?>? onChanged;
  final bool enabled;
  @override
  Widget build(BuildContext context) => InputDecorator(
    decoration: InputDecoration(labelText: label),
    child: InkWell(
      onTap: !enabled
          ? null
          : () async {
              final date = await showDatePicker(
                context: context,
                firstDate: DateTime(DateTime.now().year - 100),
                lastDate: DateTime(DateTime.now().year + 100),
                initialDate: value ?? DateTime.now(),
              );
              onChanged?.call(date);
            },
      child: Row(
        children: [
          Expanded(
            child: Text(
              value == null
                  ? 'Select date'
                  : MaterialLocalizations.of(context).formatMediumDate(value!),
            ),
          ),
          const Icon(Icons.calendar_today_outlined),
        ],
      ),
    ),
  );
}
