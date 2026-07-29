import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../domain/entities/lookup_item.dart';
import '../../domain/repositories/lookup_repository.dart';
import '../providers/lookup_provider.dart';

class SearchableLookupDropdown extends ConsumerStatefulWidget {
  const SearchableLookupDropdown({
    super.key,
    required this.type,
    this.initialValue,
  });
  final LookupType type;
  final LookupItem? initialValue;
  @override
  ConsumerState<SearchableLookupDropdown> createState() =>
      _SearchableLookupDropdownState();
}

class _SearchableLookupDropdownState
    extends ConsumerState<SearchableLookupDropdown> {
  Timer? _debounce;
  String _search = '';
  @override
  void dispose() {
    _debounce?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final result = ref.watch(
      lookupItemsProvider(LookupQuery(widget.type, search: _search)),
    );
    return AlertDialog(
      title: const Text('Select item'),
      content: SizedBox(
        width: 480,
        height: 420,
        child: Column(
          children: [
            TextField(
              autofocus: true,
              decoration: const InputDecoration(
                prefixIcon: Icon(Icons.search),
                hintText: 'Search',
              ),
              onChanged: (value) {
                _debounce?.cancel();
                _debounce = Timer(
                  const Duration(milliseconds: 300),
                  () => setState(() => _search = value),
                );
              },
            ),
            const SizedBox(height: 16),
            Expanded(
              child: result.when(
                loading: () => const Center(child: CircularProgressIndicator()),
                error: (error, stack) => Center(
                  child: TextButton(
                    onPressed: () => ref.invalidate(lookupItemsProvider),
                    child: const Text('Retry'),
                  ),
                ),
                data: (items) => items.isEmpty
                    ? const Center(child: Text('No results found'))
                    : ListView.builder(
                        itemCount: items.length,
                        itemBuilder: (context, index) => ListTile(
                          title: Text(items[index].label),
                          selected: items[index].id == widget.initialValue?.id,
                          onTap: () => Navigator.pop(context, items[index]),
                        ),
                      ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
