import 'dart:async';
import 'dart:convert';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/cupertino.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_inappwebview/flutter_inappwebview.dart';
import 'package:http/http.dart' as http;
import 'package:onesignal_flutter/onesignal_flutter.dart';

import 'analytics_service.dart';
import 'biometric_service.dart';
import 'bluetooth_service.dart';
import 'bridge_service.dart';
import 'config_service.dart';
import 'deep_link_service.dart';
import 'local_notification_service.dart';
import 'navigation_helper.dart';
import 'rating_service.dart';
import 'utils.dart';

// Build-time configuration via --dart-define (no Go templates)
const String analyticsBaseUrl = "https://api.wemob.io";
const String analyticsProjectId = String.fromEnvironment('PROJECT_ID');
const String _appUrl = String.fromEnvironment('APP_URL');
const String _splashColor = String.fromEnvironment('SPLASH_COLOR', defaultValue: '#FFFFFF');
const String _onesignalAppId = String.fromEnvironment('ONESIGNAL_APP_ID');
const bool _removeBranding = bool.fromEnvironment('REMOVE_BRANDING');

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  SystemChrome.setPreferredOrientations([DeviceOrientation.portraitUp]);

  runApp(MaterialApp(home: const WemobApp(), debugShowCheckedModeBanner: false,));
}

class WemobApp extends StatefulWidget {
  const WemobApp({super.key});

  @override
  State<WemobApp> createState() => _WemobAppState();
}

class _WemobAppState extends State<WemobApp> with WidgetsBindingObserver {
  InAppWebViewController? _controller;
  BridgeService? _bridge;
  BluetoothBridgeService? _bluetoothService;
  final String url = _appUrl;
  URLRequest? _initialUrlRequest;
  Brightness _platformBrightness = Brightness.light;
  DeepLinkService? _deepLinkService;
  InAppWebViewSettings _webViewSettings = InAppWebViewSettings(
    javaScriptEnabled: true,
    allowsInlineMediaPlayback: true,
    mediaPlaybackRequiresUserGesture: false,
    allowsBackForwardNavigationGestures: true,
    useShouldOverrideUrlLoading: true,
    supportZoom: false,
    builtInZoomControls: false,
    displayZoomControls: false,
    cacheEnabled: true,
    cacheMode: CacheMode.LOAD_DEFAULT,
    clearCache: false,
    userAgent: 'WemobApp/1.0',
  );
  bool _isLoading = true;
  bool _hasError = false;
  String? _errorMessage;
  double _progress = 0;
  AnalyticsService? _analytics;
  MobileAppConfig _config = const MobileAppConfig();
  String? _lastVisitedUrl;
  bool _sessionActive = false;
  bool _projectCheckCompleted = false;
  bool _projectActive = true;
  bool _notificationPermissionRequested = false;
  late PullToRefreshController _pullToRefreshController;
  StreamSubscription<List<ConnectivityResult>>? _connectivitySubscription;

  @override
  void initState() {
    super.initState();

    WidgetsBinding.instance.addObserver(this);
    _platformBrightness = WidgetsBinding.instance.platformDispatcher.platformBrightness;

    _pullToRefreshController = PullToRefreshController(
      settings: PullToRefreshSettings(color: Colors.black),
      onRefresh: () async {
        _controller?.reload();
      },
    );

    // Auto-reload when connectivity is restored after an error
    _connectivitySubscription = Connectivity().onConnectivityChanged.listen((results) {
      final hasConnection = results.any((r) => r != ConnectivityResult.none);
      if (hasConnection && _hasError && mounted) {
        setState(() {
          _hasError = false;
          _isLoading = true;
          _progress = 0;
        });
        _controller?.reload();
      }
    });

    _initializeApp();
  }

  Future<void> _initializeApp() async {
    // Fetch remote config first — controls which plugins to initialize
    _config = await ConfigService.fetchConfig(analyticsBaseUrl, analyticsProjectId);

    // Initialize analytics if enabled
    if (_config.analyticsEnabled) {
      _analytics = AnalyticsService(baseUrl: _config.analyticsEndpoint, projectId: analyticsProjectId);
      _analytics!.logAppOpened();
      _sessionActive = true;
    }

    // Initialize push notifications if enabled
    if (_config.pushEnabled && _config.pushAppId.isNotEmpty) {
      OneSignal.Debug.setLogLevel(OSLogLevel.none);
      OneSignal.initialize(_config.pushAppId);

      // Request notification permission once after a delay
      Future.delayed(const Duration(seconds: 5), () {
        if (mounted && !_notificationPermissionRequested) {
          _notificationPermissionRequested = true;
          OneSignal.Notifications.requestPermission(true);
        }
      });

      OneSignal.Notifications.addClickListener((event) {
        final data = event.notification.additionalData;

        final notificationId = data?['notification_id']?.toString();
        if (notificationId != null) {
          _analytics?.logPushNotificationClicked(notificationId);
        }

        final url = data?['url']?.toString();
        if (url != null) {
          final uri = Uri.tryParse(url);
          if (uri != null) {
            _controller?.loadUrl(urlRequest: URLRequest(url: WebUri(uri.toString())));
          }
        }
      });

      OneSignal.Notifications.addForegroundWillDisplayListener((event) {
        final data = event.notification.additionalData;

        final notificationId = data?['notification_id']?.toString();
        if (notificationId != null) {
          _analytics?.logPushNotificationReceived(notificationId);
        }

        event.notification.display();
      });
    }

    // Biometric auth gate
    if (_config.biometricEnabled) {
      final available = await BiometricService.isAvailable();
      if (available) {
        bool authenticated = false;
        while (!authenticated) {
          authenticated = await BiometricService.authenticate();
          if (!authenticated && mounted) {
            // Show retry — wait a moment before re-prompting
            await Future.delayed(const Duration(seconds: 1));
          }
        }
      }
    }

    // Track session for app rating
    if (_config.ratingEnabled) {
      RatingService.trackSession(_config.ratingSessions);
    }

    // Apply caching strategy from config
    if (_config.offlineEnabled) {
      switch (_config.cachingStrategy) {
        case 'cache-first':
          _webViewSettings.cacheMode = CacheMode.LOAD_CACHE_ELSE_NETWORK;
          break;
        case 'network-first':
        default:
          _webViewSettings.cacheMode = CacheMode.LOAD_DEFAULT;
          break;
      }
    }

    // Initialize deep linking if configured
    if (_config.urlScheme.isNotEmpty || _config.universalLinkDomain.isNotEmpty) {
      _deepLinkService = DeepLinkService(controllerGetter: () => _controller);
      _deepLinkService!.initialize();
    }

    _checkProjectStatus();
  }

  Future<void> _checkProjectStatus() async {
    if (analyticsProjectId.isEmpty || analyticsBaseUrl.isEmpty) {
      setState(() {
        _projectActive = true;
        _projectCheckCompleted = true;
      });
      await _initializeWebView();
      return;
    }

    final uri = Uri.parse('$analyticsBaseUrl/v1/check/$analyticsProjectId');
    bool isActive = true;

    try {
      final response = await http.get(uri).timeout(const Duration(seconds: 10));

      if (response.statusCode == 200) {
        final data = jsonDecode(response.body) as Map<String, dynamic>;
        final status = data['status']?.toString().toLowerCase();
        isActive = status == 'active';
      } else if (response.statusCode == 404) {
        isActive = false;
      }
    } catch (error) {
      isActive = true;
    }

    if (!mounted) {
      return;
    }

    setState(() {
      _projectActive = isActive;
      _projectCheckCompleted = true;
    });

    if (isActive) {
      await _initializeWebView();
    }
  }

  Future<void> _initializeWebView() async {
    setState(() {
      _initialUrlRequest = URLRequest(url: WebUri(url));
      _isLoading = true;
      _hasError = false;
      _errorMessage = null;
    });
  }

  void _setupBridge(InAppWebViewController controller) {
    // Initialize bluetooth service if any bluetooth methods are enabled
    final methods = Set.from(_config.bridgeMethods);
    if (methods.any((m) => m.toString().startsWith('bluetooth.'))) {
      _bluetoothService = BluetoothBridgeService();
    }

    _bridge = BridgeService(
      controller: controller,
      allowedMethods: methods,
      projectId: analyticsProjectId,
      bluetoothService: _bluetoothService,
    );
    _bridge!.register();
  }

  Future<void> _injectBridgeSDK(InAppWebViewController controller) async {
    // Inject the bridge SDK so the web page can call WemobBridge methods
    await controller.evaluateJavascript(source: '''
      if (!window.WemobBridge) {
        (function() {
          var isNative = function() { return !!(window.flutter_inappwebview && window.flutter_inappwebview.callHandler); };
          var call = function(method, args) {
            if (!isNative()) return Promise.resolve(null);
            return window.flutter_inappwebview.callHandler("WemobBridge", { method: method, args: args || {} });
          };
          window.WemobBridge = {
            isNative: isNative,
            call: call,
            getDeviceInfo: function() { return call("getDeviceInfo"); },
            share: function(text, url) { return call("share", { text: text, url: url }); },
            haptic: function(type) { return call("haptic", { type: type || "light" }); },
            openExternal: function(url) { return call("openExternal", { url: url }); },
            getConnectionStatus: function() { return call("getConnectionStatus"); },
            getTheme: function() { return call("getTheme"); },
            requestReview: function() { return call("requestReview"); },
            localNotification: {
              show: function(title, body, id) { return call("localNotification.show", { title: title, body: body, id: id || 0 }); },
              cancel: function(id) { return call("localNotification.cancel", { id: id || 0 }); },
              cancelAll: function() { return call("localNotification.cancelAll"); }
            },
            storage: {
              get: function(key) { return call("storage.get", { key: key }); },
              set: function(key, value) { return call("storage.set", { key: key, value: value }); },
              remove: function(key) { return call("storage.remove", { key: key }); }
            },
            camera: {
              takePhoto: function(opts) { return call("camera.takePhoto", opts || {}); },
              pickImage: function(opts) { return call("camera.pickImage", opts || {}); },
              pickVideo: function() { return call("camera.pickVideo"); }
            },
            contacts: {
              getAll: function() { return call("contacts.getAll"); },
              pick: function() { return call("contacts.pick"); }
            },
            calendar: {
              addEvent: function(opts) { return call("calendar.addEvent", opts || {}); }
            },
            files: {
              pick: function(opts) { return call("files.pick", opts || {}); }
            },
            bluetooth: {
              isAvailable: function() { return call("bluetooth.isAvailable"); },
              scan: function(opts) { return call("bluetooth.scan", opts || {}); },
              connect: function(deviceId) { return call("bluetooth.connect", { deviceId: deviceId }); },
              disconnect: function(deviceId) { return call("bluetooth.disconnect", { deviceId: deviceId }); },
              getServices: function(deviceId) { return call("bluetooth.getServices", { deviceId: deviceId }); },
              read: function(deviceId, serviceUuid, characteristicUuid) { return call("bluetooth.read", { deviceId: deviceId, serviceUuid: serviceUuid, characteristicUuid: characteristicUuid }); },
              write: function(deviceId, serviceUuid, characteristicUuid, value) { return call("bluetooth.write", { deviceId: deviceId, serviceUuid: serviceUuid, characteristicUuid: characteristicUuid, value: value }); }
            }
          };
        })();
      }
    ''');
  }

  Widget _buildSplash(Color background, bool isDark) {
    return Container(
      width: double.infinity,
      color: background,
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 90,
            height: 90,
            decoration: BoxDecoration(
              borderRadius: BorderRadius.circular(20),
              image: DecorationImage(
                image: AssetImage("assets/icon/app_icon.png"),
                fit: BoxFit.cover,
              ),
            ),
          ),

          const SizedBox(height: 48),
          CircularProgressIndicator(color: isDark ? Colors.white : Colors.black, strokeWidth: 4),
          const SizedBox(height: 48),
          if (!_removeBranding) ...[
            Text("Created by", style: TextStyle(color: isDark ? Colors.white : Colors.black54)),
            SizedBox(width: 128, height: 48, child: isDark ? Image.asset("assets/icon/wemob-logo.png") : Image.asset("assets/icon/wemob-logo-dark.png")),
          ],
        ],
      ),
    );
  }

  Widget _buildLoadingOverlay(Color background, bool isDark) {
    return Positioned.fill(
      child: IgnorePointer(
        ignoring: !_isLoading,
        child: AnimatedOpacity(
          opacity: _isLoading ? 1.0 : 0.0,
          duration: const Duration(milliseconds: 500),
          child: _buildSplash(background, isDark),
        ),
      ),
    );
  }

  Widget _buildErrorState(Color background, bool isDark) {
    final textColor = isDark ? Colors.white : Colors.black;
    return Container(
      width: double.infinity,
      color: background,
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 90,
            height: 90,
            decoration: BoxDecoration(
              borderRadius: BorderRadius.circular(20),
              image: DecorationImage(
                image: AssetImage("assets/icon/app_icon.png"),
                fit: BoxFit.cover,
              ),
            ),
          ),
          const SizedBox(height: 32),
          Icon(CupertinoIcons.wifi_slash, size: 48, color: textColor.withValues(alpha: 0.5)),
          const SizedBox(height: 16),
          Text(
            _errorMessage ?? 'Unable to load content',
            textAlign: TextAlign.center,
            style: TextStyle(color: textColor.withValues(alpha: 0.7), fontSize: 16),
          ),
          const SizedBox(height: 24),
          TextButton(
            onPressed: () {
              setState(() {
                _hasError = false;
                _isLoading = true;
                _progress = 0;
              });
              _controller?.reload();
            },
            style: TextButton.styleFrom(
              backgroundColor: textColor.withValues(alpha: 0.1),
              padding: const EdgeInsets.symmetric(horizontal: 32, vertical: 12),
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(24)),
            ),
            child: Text('Retry', style: TextStyle(color: textColor, fontSize: 16)),
          ),
        ],
      ),
    );
  }

  Widget _buildInactiveMessage(Color background, bool isDark) {
    final textColor = isDark ? Colors.white : Colors.black;
    return Container(
      color: background,
      alignment: Alignment.center,
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 90,
            height: 90,
            decoration: BoxDecoration(
              borderRadius: BorderRadius.circular(20),
              image: DecorationImage(
                image: AssetImage("assets/icon/app_icon.png"),
                fit: BoxFit.cover,
              ),
            ),
          ),
          const SizedBox(height: 32),
          Text(
            'This app is not active.\nCheck wemob.io and contact the app owner.\n\nsupport@wemob.io',
            textAlign: TextAlign.center,
            style: TextStyle(color: textColor, fontWeight: FontWeight.w600),
          ),
        ],
      ),
    );
  }

  Widget _buildBodyContent(Color externalColor, bool isDark) {
    if (!_projectCheckCompleted) {
      return _buildSplash(externalColor, isDark);
    }

    if (!_projectActive) {
      return _buildInactiveMessage(externalColor, isDark);
    }

    final initialRequest = _initialUrlRequest;
    if (initialRequest == null) {
      return _buildSplash(externalColor, isDark);
    }

    if (_hasError && !_isLoading) {
      return _buildErrorState(externalColor, isDark);
    }

    return Stack(
      children: [
        InAppWebView(
          initialUrlRequest: initialRequest,
          initialSettings: _webViewSettings,
          pullToRefreshController: _config.pullToRefreshEnabled ? _pullToRefreshController : null,
          onWebViewCreated: (controller) {
            _controller = controller;
            _setupBridge(controller);
          },
          onProgressChanged: (controller, progress) {
            if (!mounted) return;
            setState(() { _progress = progress / 100; });
          },
          onLoadStop: (controller, uri) {
            if (!mounted) return;
            setState(() { _isLoading = false; });
            _pullToRefreshController.endRefreshing();
            if (uri != null) {
              _logVisitedUrl(uri.toString());
            }
            // Inject bridge SDK into the page after load
            _injectBridgeSDK(controller);
          },
          shouldOverrideUrlLoading: (controller, navigationAction) async {
            final decision = await handleNavigation(navigationAction);
            if (decision == NavigationActionPolicy.ALLOW) {
              final targetUrl = navigationAction.request.url;
              if (targetUrl != null) {
                _logVisitedUrl(targetUrl.toString());
              }
            }
            return decision;
          },
          onReceivedHttpError: (controller, request, errorResponse) {
            final statusCode = errorResponse.statusCode ?? 0;
            if (statusCode >= 500 && _isLoading) {
              if (!mounted) return;
              setState(() {
                _isLoading = false;
                _hasError = true;
                _errorMessage = 'Server error ($statusCode). Please try again.';
              });
              _pullToRefreshController.endRefreshing();
            }
          },
          onReceivedError: (controller, request, error) {
            // Only handle errors for the main frame URL
            final errorUrl = request.url?.toString() ?? '';
            if (errorUrl == url || _isLoading) {
              if (!mounted) return;
              setState(() {
                _isLoading = false;
                _hasError = true;
                _errorMessage = _describeWebResourceError(error);
              });
              _pullToRefreshController.endRefreshing();
            }
          },
        ),
        // Progress bar
        if (_isLoading && _progress > 0 && _progress < 1.0)
          Positioned(
            top: 0,
            left: 0,
            right: 0,
            child: LinearProgressIndicator(
              value: _progress,
              minHeight: 2,
              backgroundColor: Colors.transparent,
              valueColor: AlwaysStoppedAnimation<Color>(isDark ? Colors.white : Colors.black),
            ),
          ),
        _buildLoadingOverlay(externalColor, isDark),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final Color externalColor = parseHexColor(_splashColor);
    final bool isDark = isDarkColor(externalColor);
    final overlayStyle = isDark ? SystemUiOverlayStyle.light : SystemUiOverlayStyle.dark;

    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, result) async {
        if (didPop) return;

        if (_controller == null) {
          SystemNavigator.pop();
          return;
        }

        final canGoBack = await _controller!.canGoBack();
        if (canGoBack) {
          _controller!.goBack();
        } else {
          SystemNavigator.pop();
        }
      },
      child: Scaffold(
        backgroundColor: externalColor,
        body: AnnotatedRegion<SystemUiOverlayStyle>(
          value: overlayStyle,
          child: SafeArea(child: _buildBodyContent(externalColor, isDark)),
        ),
      ),
    );
  }

  void _logVisitedUrl(String url) {
    if (_lastVisitedUrl == url) {
      return;
    }
    _lastVisitedUrl = url;
    _analytics?.logUrlVisited(url);
  }

  void _startSession() {
    if (_sessionActive) {
      return;
    }
    _sessionActive = true;
    _analytics?.logSessionStarted();
  }

  void _endSession() {
    if (!_sessionActive) {
      return;
    }
    _sessionActive = false;
    _analytics?.logSessionEnded();
  }

  String _describeWebResourceError(WebResourceError error) {
    final type = error.type;
    if (type == WebResourceErrorType.HOST_LOOKUP) {
      return 'Could not find the server. Check your internet connection.';
    } else if (type == WebResourceErrorType.TIMEOUT) {
      return 'Connection timed out. Please try again.';
    } else if (type == WebResourceErrorType.SECURE_CONNECTION_FAILED ||
        type == WebResourceErrorType.SERVER_CERTIFICATE_UNTRUSTED ||
        type == WebResourceErrorType.FAILED_SSL_HANDSHAKE) {
      return 'Secure connection failed. Please try again later.';
    } else if (type == WebResourceErrorType.NOT_CONNECTED_TO_INTERNET ||
        type == WebResourceErrorType.NETWORK_CONNECTION_LOST) {
      return 'No internet connection. Check your network settings.';
    }
    return 'Unable to load content. Please try again.';
  }

  @override
  void didChangePlatformBrightness() {
    if (_config.darkModeEnabled) {
      setState(() {
        _platformBrightness = WidgetsBinding.instance.platformDispatcher.platformBrightness;
      });
      // Notify web page of theme change
      _controller?.evaluateJavascript(source: '''
        if (window.WemobBridge && window.WemobBridge._onThemeChange) {
          window.WemobBridge._onThemeChange("${_platformBrightness == Brightness.dark ? 'dark' : 'light'}");
        }
      ''');
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _startSession();
    } else if (state == AppLifecycleState.paused ||
        state == AppLifecycleState.inactive ||
        state == AppLifecycleState.detached) {
      _endSession();
    }
  }

  @override
  void dispose() {
    _connectivitySubscription?.cancel();
    _deepLinkService?.dispose();
    _bluetoothService?.dispose();
    WidgetsBinding.instance.removeObserver(this);
    _endSession();
    _analytics?.dispose();
    super.dispose();
  }
}
