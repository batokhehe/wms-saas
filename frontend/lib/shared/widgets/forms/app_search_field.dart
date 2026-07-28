import 'package:flutter/material.dart';

class AppSearchField extends StatelessWidget {
  const AppSearchField({
    super.key,
    this.controller,
    this.onChanged,
    this.hintText = 'Search',
  });
  final TextEditingController? controller;
  final ValueChanged<String>? onChanged;
  final String hintText;
  @override
  Widget build(BuildContext context) => TextField(
    controller: controller,
    onChanged: onChanged,
    decoration: InputDecoration(
      prefixIcon: const Icon(Icons.search),
      hintText: hintText,
    ),
  );
}
