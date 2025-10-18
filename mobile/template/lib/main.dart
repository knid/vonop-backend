import 'dart:convert';

import 'package:flutter/cupertino.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:http/http.dart' as http;
import 'package:onesignal_flutter/onesignal_flutter.dart';
import 'package:webview_flutter/webview_flutter.dart';
import 'package:webview_flutter_wkwebview/webview_flutter_wkwebview.dart';

import 'analytics_service.dart';
import 'navigation_helper.dart';
import 'utils.dart';

const String analyticsBaseUrl = "https://api.wemob.io";
const String analyticsProjectId = "{{ WEMOB_PROJECT_ID }}";

void main() {
  runApp(MaterialApp(home: const WemobApp()));

  OneSignal.Debug.setLogLevel(OSLogLevel.verbose);
  OneSignal.initialize("{{ ONESIGNAL_APP_ID }}");
}

class WemobApp extends StatefulWidget {
  const WemobApp({super.key});

  @override
  State<WemobApp> createState() => _WemobAppState();
}

class _WemobAppState extends State<WemobApp> with WidgetsBindingObserver {
  WebViewController? _controller;
  final String url = "{{ WEMOB_PROJECT_URL }}";
  bool _isLoading = true;
  late final AnalyticsService _analytics;
  String? _lastVisitedUrl;
  bool _sessionActive = false;
  bool _projectCheckCompleted = false;
  bool _projectActive = true;

  @override
  void initState() {
    super.initState();

    WidgetsBinding.instance.addObserver(this);

    _analytics = AnalyticsService(baseUrl: analyticsBaseUrl, projectId: analyticsProjectId);
    _analytics.logAppOpened();
    _startSession();
    _checkProjectStatus();

    OneSignal.Notifications.addClickListener((event) {
      final data = event.notification.additionalData;

      final notificationId = data?['notification_id']?.toString();
      if (notificationId != null) {
        _analytics.logPushNotificationClicked(notificationId);
      }

      final url = data?['url']?.toString();
      if (url != null) {
        _controller?.loadRequest(Uri.parse(url));
      }
    });

    OneSignal.Notifications.addForegroundWillDisplayListener((event) {
      final data = event.notification.additionalData;

      final notificationId = data?['notification_id']?.toString();
      if (notificationId != null) {
        _analytics.logPushNotificationReceived(notificationId);
      }

      event.notification.display();
    });
  }

  Future<void> _checkProjectStatus() async {
    if (analyticsProjectId.isEmpty || analyticsBaseUrl.isEmpty) {
      setState(() {
        _projectActive = true;
        _projectCheckCompleted = true;
      });
      _initializeWebView();
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
      debugPrint('Project status check failed: $error');
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
      _initializeWebView();
    }
  }

  void _initializeWebView() {
    late final PlatformWebViewControllerCreationParams params;
    if (WebViewPlatform.instance is WebKitWebViewPlatform) {
      params = WebKitWebViewControllerCreationParams(
        allowsInlineMediaPlayback: true,
        mediaTypesRequiringUserAction: const <PlaybackMediaTypes>{},
      );
    } else {
      params = const PlatformWebViewControllerCreationParams();
    }

    final controller = WebViewController.fromPlatformCreationParams(params);
    controller
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      ..setNavigationDelegate(
        NavigationDelegate(
          onPageFinished: (String url) {
            setState(() {
              _isLoading = false;
            });
            _logVisitedUrl(url);
            Future.delayed(const Duration(milliseconds: 3000), () {
              OneSignal.Notifications.requestPermission(true);
            });
          },
          onHttpError: (HttpResponseError error) {},
          onWebResourceError: (WebResourceError error) {},
          onNavigationRequest: (request) async {
            final decision = await handleNavigation(request);
            if (decision == NavigationDecision.navigate) {
              _logVisitedUrl(request.url);
              setState(() {
                _isLoading = true;
              });
            }
            return decision;
          },
        ),
      )
      ..loadRequest(Uri.parse(url));

    if (controller.platform is WebKitWebViewController) {
      (controller.platform as WebKitWebViewController).setAllowsBackForwardNavigationGestures(true);
    }

    setState(() {
      _controller = controller;
      _isLoading = true;
    });
  }

  Widget _buildSplash(Color background, bool isDark) {
    debugPrint("_buildSplash");
    return Container(
      width: double.infinity,
      color: background,
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Container(
            width: 80,
            height: 80,
            decoration: BoxDecoration(
              color: Colors.grey.shade200,
              borderRadius: BorderRadius.circular(16),
            ),
            child: const Icon(Icons.apps, size: 48, color: Colors.black),
          ),
          const SizedBox(height: 64),
          CircularProgressIndicator(color: isDark ? Colors.white : Colors.black, strokeWidth: 3),
          const SizedBox(height: 96),
          Text("Powered by", style: TextStyle(color: isDark ? Colors.white : Colors.black54)),
          Text(
            "Wemob.io",
            style: TextStyle(
              color: isDark ? Colors.white : Colors.black54,
              fontWeight: FontWeight.bold,
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildLoadingOverlay(Color background, bool isDark) {
    debugPrint("_buildLoadingOverlay");
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

  Widget _buildInactiveMessage(Color background, bool isDark) {
    return Container(
      color: background,
      alignment: Alignment.center,
      child: Text(
        'This app is not active. check wemob.io',
        textAlign: TextAlign.center,
        style: TextStyle(color: isDark ? Colors.white : Colors.black, fontWeight: FontWeight.w600),
      ),
    );
  }

  Widget _buildBodyContent(Color externalColor, bool isDark) {
    if (!_projectCheckCompleted) {
      debugPrint("first");
      return _buildSplash(externalColor, isDark);
    }

    if (!_projectActive) {
      return _buildInactiveMessage(externalColor, isDark);
    }

    final controller = _controller;
    if (controller == null) {
      return _buildSplash(externalColor, isDark);
    }

    debugPrint("second");
    return Stack(
      children: [
        WebViewWidget(controller: controller),
        _buildLoadingOverlay(externalColor, isDark),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final String externalHexColor = "{{ WEMOB_SPLASH_SCREEN_COLOR }}";
    final Color externalColor = Color(int.parse(externalHexColor.replaceFirst('#', '0xff')));
    // final externalColor = CupertinoColors.darkBackgroundGray;
    final bool isDark = isDarkColor(externalColor);
    debugPrint("isDark: $isDark");
    final overlayStyle = isDark ? SystemUiOverlayStyle.light : SystemUiOverlayStyle.dark;

    return Scaffold(
      backgroundColor: externalColor,
      body: AnnotatedRegion<SystemUiOverlayStyle>(
        value: overlayStyle,
        child: SafeArea(child: _buildBodyContent(externalColor, isDark)),
      ),
    );
  }

  void _logVisitedUrl(String url) {
    if (_lastVisitedUrl == url) {
      return;
    }
    _lastVisitedUrl = url;
    _analytics.logUrlVisited(url);
  }

  void _startSession() {
    if (_sessionActive) {
      return;
    }
    _sessionActive = true;
    _analytics.logSessionStarted();
  }

  void _endSession() {
    if (!_sessionActive) {
      return;
    }
    _sessionActive = false;
    _analytics.logSessionEnded();
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
    WidgetsBinding.instance.removeObserver(this);
    _endSession();
    super.dispose();
  }
}
