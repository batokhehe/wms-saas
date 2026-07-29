/// Paths reserved for modules that are not built yet.
///
/// A module leaves this list the moment it gets a real `GoRoute` in
/// `app_router.dart` and a `location` on its sidebar entry — warehouse,
/// location, product, supplier, customer, inventory and the inventory ledger
/// have all done so. What remains here is genuinely unbuilt: the sidebar shows
/// those entries with no destination and explains that they are coming.
abstract final class FutureRoutes {
  static const purchaseOrder = '/purchase-orders';
  static const asn = '/asn';
  static const receiving = '/receiving';
  static const putAway = '/put-away';
  static const salesOrder = '/sales-orders';
  static const picking = '/picking';
  static const packing = '/packing';
  static const shipping = '/shipping';
  static const reports = '/reports';
  static const settings = '/settings';
}
