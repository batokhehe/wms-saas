import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import '../../../../core/network/api_client.dart';
import '../../domain/entities/current_user.dart';

final tokenStorageProvider = Provider(
  (ref) => TokenStorage(const FlutterSecureStorage()),
);
final apiClientProvider = Provider(
  (ref) => ApiClient(ref.read(tokenStorageProvider)),
);
final currentUserProvider = AsyncNotifierProvider<AuthController, CurrentUser?>(
  AuthController.new,
);

class AuthController extends AsyncNotifier<CurrentUser?> {
  ApiClient get _api => ref.read(apiClientProvider);
  TokenStorage get _tokens => ref.read(tokenStorageProvider);
  @override
  Future<CurrentUser?> build() async {
    if (await _tokens.accessToken() == null) return null;
    return _me();
  }

  Future<void> login(String email, String password) async {
    state = const AsyncLoading();
    state = await AsyncValue.guard(() async {
      final response = await _api.dio.post(
        '/auth/login',
        data: {'email': email, 'password': password, 'device': 'Flutter Web'},
      );
      final data = response.data['data'] as Map<String, dynamic>;
      final pair = data['tokens'] as Map<String, dynamic>;
      await _tokens.save(
        pair['access_token'] as String,
        pair['refresh_token'] as String,
      );
      return CurrentUser.fromJson(data['user'] as Map<String, dynamic>);
    });
  }

  Future<CurrentUser?> _me() async {
    try {
      final response = await _api.dio.get('/auth/me');
      return CurrentUser.fromJson(
        response.data['data'] as Map<String, dynamic>,
      );
    } on DioException {
      await _tokens.clear();
      return null;
    }
  }

  Future<void> logout() async {
    final refresh = await _tokens.refreshToken();
    if (refresh != null) {
      try {
        await _api.dio.post('/auth/logout', data: {'refresh_token': refresh});
      } on DioException {}
    }
    await _tokens.clear();
    state = const AsyncData(null);
  }
}
