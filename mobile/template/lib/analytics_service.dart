import 'dart:convert';
import 'dart:io';

import 'package:device_info_plus/device_info_plus.dart';
import 'package:flutter/foundation.dart';
import 'package:http/http.dart' as http;
import 'package:package_info_plus/package_info_plus.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:uuid/uuid.dart';

class AnalyticsService {
  AnalyticsService({required this.baseUrl, required this.projectId});

  final String baseUrl;
  final String projectId;

  static const _deviceIdKey = 'wemob_device_id';
  static const _uuid = Uuid();

  String? _deviceId;
  String? _appVersion;
  String? _platform;
  String? _model;

  Future<void>? _initialization;

  Future<void> _ensureInitialized() async {
    _initialization ??= _initialize();
    try {
      await _initialization;
    } catch (e) {
      debugPrint('Analytics initialization failed: $e');
      rethrow;
    }
  }

  Future<void> _initialize() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final storedId = prefs.getString(_deviceIdKey);
      if (storedId != null && storedId.isNotEmpty) {
        _deviceId = storedId;
      } else {
        final newId = _uuid.v4();
        await prefs.setString(_deviceIdKey, newId);
        _deviceId = newId;
      }

      final packageInfo = await PackageInfo.fromPlatform();
      _appVersion = packageInfo.version;
      _platform = _resolvePlatform();
      _model = await _resolveModel();
    } catch (error) {
      debugPrint('Error initializing analytics service: $error');
      rethrow;
    }
  }

  String _resolvePlatform() {
    if (kIsWeb) {
      return 'web';
    }
    if (Platform.isAndroid) {
      return 'android';
    }
    if (Platform.isIOS) {
      return 'ios';
    }
    if (Platform.isMacOS) {
      return 'macos';
    }
    if (Platform.isWindows) {
      return 'windows';
    }
    if (Platform.isLinux) {
      return 'linux';
    }
    return 'unknown';
  }

  Future<String> _resolveModel() async {
    if (kIsWeb) {
      return 'web';
    }

    final deviceInfoPlugin = DeviceInfoPlugin();
    try {
      if (Platform.isAndroid) {
        final androidInfo = await deviceInfoPlugin.androidInfo;
        return androidInfo.model ?? 'android';
      }
      if (Platform.isIOS) {
        final iosInfo = await deviceInfoPlugin.iosInfo;
        return iosInfo.utsname.machine ?? iosInfo.model ?? 'ios';
      }
      if (Platform.isMacOS) {
        final macInfo = await deviceInfoPlugin.macOsInfo;
        return macInfo.model;
      }
      if (Platform.isWindows) {
        final windowsInfo = await deviceInfoPlugin.windowsInfo;
        return windowsInfo.productName ?? 'windows';
      }
      if (Platform.isLinux) {
        final linuxInfo = await deviceInfoPlugin.linuxInfo;
        return linuxInfo.prettyName ?? 'linux';
      }
    } catch (error) {
      debugPrint('Failed to resolve device model: $error');
    }
    return 'unknown';
  }

  Future<void> logAppOpened() => _logEvent('app_opened');

  Future<void> logSessionStarted() => _logEvent('session_started');

  Future<void> logSessionEnded() => _logEvent('session_ended');

  Future<void> logUrlVisited(String url) => _logEvent('url_visited');

  Future<void> logPushNotificationReceived() => _logEvent('push_notification_received');

  Future<void> logPushNotificationClicked() => _logEvent('push_notification_clicked');

  Future<void> _logEvent(String eventType) async {
    if (projectId.isEmpty || baseUrl.isEmpty) {
      debugPrint('Analytics skipped: missing configuration for event $eventType');
      return;
    }

    try {
      await _ensureInitialized();
    } catch (_) {
      // Initialization failure already logged. Skip sending event.
      return;
    }

    final deviceId = _deviceId;
    final appVersion = _appVersion;
    final platform = _platform;
    final model = _model;

    if (deviceId == null || appVersion == null || platform == null || model == null) {
      debugPrint('Analytics skipped: incomplete metadata for event $eventType');
      return;
    }

    final uri = Uri.parse('$baseUrl/v1/projects/{{ WEMOB_PROJECT_ID }}/analytics/events');
    final body = jsonEncode({
      'event_type': eventType,
      'device_id': deviceId,
      'app_version': appVersion,
      'platform': platform,
      'model': model,
    });

    try {
      final response = await http
          .post(uri, headers: const {'Content-Type': 'application/json'}, body: body)
          .timeout(const Duration(seconds: 10));

      if (response.statusCode >= 400) {
        debugPrint(
          'Failed to log analytics event "$eventType": ${response.statusCode} ${response.reasonPhrase}',
        );
      }
    } catch (error) {
      debugPrint('Error sending analytics event "$eventType": $error');
    }
  }
}
