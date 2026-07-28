import 'package:dio/dio.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

class TokenStorage {
  const TokenStorage(this._storage);
  final FlutterSecureStorage _storage;
  static const _accessKey = 'access_token';
  static const _refreshKey = 'refresh_token';
  Future<String?> accessToken() => _storage.read(key: _accessKey);
  Future<String?> refreshToken() => _storage.read(key: _refreshKey);
  Future<void> save(String access, String refresh) async {
    await _storage.write(key: _accessKey, value: access);
    await _storage.write(key: _refreshKey, value: refresh);
  }

  Future<void> clear() => _storage.deleteAll();
}

class ApiClient {
  ApiClient(this.tokens)
    : dio = Dio(
        BaseOptions(
          baseUrl: '/api/v1',
          connectTimeout: const Duration(seconds: 15),
          receiveTimeout: const Duration(seconds: 15),
        ),
      ) {
    dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) async {
          final token = await tokens.accessToken();
          if (token != null) options.headers['Authorization'] = 'Bearer $token';
          handler.next(options);
        },
        onError: (error, handler) async {
          if (error.response?.statusCode != 401 ||
              error.requestOptions.extra['retried'] == true ||
              error.requestOptions.path.contains('/auth/refresh'))
            return handler.next(error);
          final refresh = await tokens.refreshToken();
          if (refresh == null) return handler.next(error);
          try {
            final response = await dio.post(
              '/auth/refresh',
              data: {'refresh_token': refresh},
            );
            final data = response.data['data']['tokens'];
            await tokens.save(
              data['access_token'] as String,
              data['refresh_token'] as String,
            );
            final retry = error.requestOptions;
            retry.extra['retried'] = true;
            retry.headers['Authorization'] = 'Bearer ${data['access_token']}';
            return handler.resolve(await dio.fetch(retry));
          } catch (_) {
            await tokens.clear();
            return handler.next(error);
          }
        },
      ),
    );
  }
  final TokenStorage tokens;
  final Dio dio;
}

String apiError(DioException error) =>
    error.response?.data is Map<String, dynamic>
    ? (error.response!.data['message'] as String? ?? 'Request failed')
    : 'Unable to reach the server.';
