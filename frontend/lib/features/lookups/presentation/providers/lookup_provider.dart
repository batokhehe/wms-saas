import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../auth/presentation/controllers/auth_controller.dart';
import '../../data/datasources/lookup_remote_datasource.dart';
import '../../data/repositories/lookup_repository_impl.dart';
import '../../domain/entities/lookup_item.dart';
import '../../domain/entities/lookup_availability.dart';
import '../../domain/repositories/lookup_repository.dart';
import '../../domain/usecases/get_lookup_items.dart';

final lookupRepositoryProvider = Provider<LookupRepository>(
  (ref) =>
      LookupRepositoryImpl(LookupRemoteDatasource(ref.read(apiClientProvider))),
);
final getLookupItemsProvider = Provider(
  (ref) => GetLookupItems(ref.read(lookupRepositoryProvider)),
);
final lookupItemsProvider = FutureProvider.autoDispose
    .family<List<LookupItem>, LookupQuery>(
      (ref, query) => ref.read(getLookupItemsProvider)(
        query.type,
        search: query.search,
        refresh: query.refresh,
      ),
    );

class LookupQuery {
  const LookupQuery(this.type, {this.search = '', this.refresh = false});
  final LookupType type;
  final String search;
  final bool refresh;
  @override
  bool operator ==(Object other) =>
      other is LookupQuery &&
      other.type == type &&
      other.search == search &&
      other.refresh == refresh;
  @override
  int get hashCode => Object.hash(type, search, refresh);
}

/// Typed placeholders for backend lookup capabilities that are not exposed.
/// Product UI can disable the corresponding selector without sending a request.
final uomLookupAvailabilityProvider = Provider<LookupAvailability>(
  (ref) => const LookupUnavailable('UOM lookup endpoint is not available.'),
);
final categoryLookupAvailabilityProvider = Provider<LookupAvailability>(
  (ref) => const LookupUnavailable('Category lookup endpoint is not available.'),
);
final brandLookupAvailabilityProvider = Provider<LookupAvailability>(
  (ref) => const LookupUnavailable('Brand lookup endpoint is not available.'),
);
