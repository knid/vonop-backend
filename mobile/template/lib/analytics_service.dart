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
  String? _sessionId;

  Future<void>? _initialization;
  Future<void>? _sessionInitialization;

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

  Future<void> logAppOpened() => _logEvent('app_opened', startNewSession: true);

  Future<void> logSessionStarted() => _logEvent('session_started');

  Future<void> logSessionEnded() => _logEvent('session_ended');

  Future<void> logUrlVisited(String url) => _logEvent('url_visited', data: {'url': url});

  // Optional notificationId can be provided by callers when available.
  Future<void> logPushNotificationReceived([String? notificationId]) => _logEventWithoutSession(
    'push_notification_received',
    notificationId != null ? {'notification_id': notificationId} : null,
  );

  Future<void> logPushNotificationClicked([String? notificationId]) => _logEventWithoutSession(
    'push_notification_clicked',
    notificationId != null ? {'notification_id': notificationId} : null,
  );

  Future<void> _logEventWithoutSession(String eventType, [Map<String, dynamic>? data]) async {
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

    await _sendEvent(eventType, 'none', data);
  }

  Future<void> _logEvent(
    String eventType, {
    bool startNewSession = false,
    Map<String, dynamic>? data,
  }) async {
    if (projectId.isEmpty || baseUrl.isEmpty) {
      debugPrint('Analytics skipped: missing configuration for event $eventType');
      return;
    }

    try {
      await _ensureInitialized();
      if (startNewSession) {
        await _startNewSession();
      } else {
        await _waitForActiveSession();
      }
    } catch (_) {
      // Initialization failure already logged. Skip sending event.
      return;
    }

    final sessionId = _sessionId;

    if (sessionId == null || sessionId.isEmpty) {
      debugPrint('Analytics skipped: missing session id for event $eventType');
      return;
    }

    await _sendEvent(eventType, sessionId, data);
  }

  Future<void> _sendEvent(String eventType, String sessionId, [Map<String, dynamic>? data]) async {
    final uri = Uri.parse('$baseUrl/v1/projects/$projectId/analytics/events');

    // Build the `data` object for the backend. Start with device/app metadata
    // and merge any event-specific data provided by the caller. Caller data
    // overrides metadata when keys collide.
    final Map<String, dynamic> payloadData = {};

    if (sessionId == "none") {
      if (_deviceId != null) payloadData['device_id'] = _deviceId;
      if (_appVersion != null) payloadData['app_version'] = _appVersion;
      if (_platform != null) payloadData['platform'] = _platform;
      if (_model != null) payloadData['model'] = _model;
    }

    if (data != null) payloadData.addAll(data);

    final body = jsonEncode({
      'event_type': eventType,
      'session_id': sessionId,
      'data': payloadData,
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

  Future<void> _startNewSession() async {
    _sessionId = null;
    final creation = _createSession();
    _sessionInitialization = creation;

    try {
      await creation;
    } catch (error) {
      _sessionInitialization = null;
      debugPrint('Analytics session initialization failed: $error');
      rethrow;
    }
  }

  Future<void> _waitForActiveSession() async {
    final sessionFuture = _sessionInitialization;
    if (sessionFuture != null) {
      await sessionFuture;
    }
  }

  Future<void> _createSession() async {
    final deviceId = _deviceId;
    final appVersion = _appVersion;
    final platform = _platform;
    final model = _model;

    if (deviceId == null || appVersion == null || platform == null || model == null) {
      throw StateError('Analytics session skipped: device metadata is incomplete');
    }

    final uri = Uri.parse('$baseUrl/v1/projects/$projectId/analytics/session');
    final body = jsonEncode({
      'device_id': deviceId,
      'app_version': appVersion,
      'platform': platform,
      'model': model,
    });

    http.Response response;
    try {
      response = await http
          .post(uri, headers: const {'Content-Type': 'application/json'}, body: body)
          .timeout(const Duration(seconds: 10));
    } catch (error) {
      throw StateError('Error creating analytics session: $error');
    }

    if (response.statusCode >= 400) {
      throw StateError(
        'Failed to create analytics session: ${response.statusCode} ${response.reasonPhrase}',
      );
    }

    try {
      final parsed = jsonDecode(response.body);
      if (parsed is Map<String, dynamic>) {
        final sessionId = parsed['session_id'];
        if (sessionId is String && sessionId.isNotEmpty) {
          _sessionId = sessionId;
          return;
        }
      }
      throw const FormatException('Missing session_id in analytics session response');
    } catch (error) {
      throw StateError('Failed to parse analytics session response: $error');
    }
  }
}
