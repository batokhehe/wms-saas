class CurrentUser {
  const CurrentUser({
    required this.id,
    required this.email,
    required this.fullName,
  });
  final String id, email, fullName;
  factory CurrentUser.fromJson(Map<String, dynamic> json) => CurrentUser(
    id: json['id'] as String,
    email: json['email'] as String,
    fullName: json['full_name'] as String,
  );
}
