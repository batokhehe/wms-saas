import 'lookup_item.dart';

sealed class LookupAvailability {
  const LookupAvailability();
}

class LookupAvailable extends LookupAvailability {
  const LookupAvailable(this.items);
  final List<LookupItem> items;
}

class LookupUnavailable extends LookupAvailability {
  const LookupUnavailable(this.reason);
  final String reason;
}
