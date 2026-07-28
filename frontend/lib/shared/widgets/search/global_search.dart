import 'package:flutter/material.dart';

import '../forms/app_search_field.dart';

class GlobalSearch extends StatelessWidget {
  const GlobalSearch({super.key, this.onChanged});
  final ValueChanged<String>? onChanged;
  @override
  Widget build(BuildContext context) =>
      AppSearchField(hintText: 'Search across WMS', onChanged: onChanged);
}
