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
const String navigationConfigUrl = "$analyticsBaseUrl/projects/$analyticsProjectId/public/navbar";

class NavItem {
  const NavItem({
    required this.label,
    required this.url,
    required this.icon,
    required this.order,
  });

  final String label;
  final String url;
  final String icon;
  final int order;

  factory NavItem.fromJson(Map<String, dynamic> json) {
    return NavItem(
      label: json['label']?.toString() ?? '',
      url: json['url']?.toString() ?? '',
      icon: json['icon']?.toString() ?? '',
      order: int.tryParse(json['order']?.toString() ?? '') ?? 0,
    );
  }
}

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
  List<NavItem> _navItems = <NavItem>[];
  int _selectedNavIndex = 0;

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
      await _initializeWebView();
    }
  }

  Future<void> _initializeWebView() async {
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
                // _isLoading = true;
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

    await _loadNavigationItems();
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
            width: 90,
            margin: EdgeInsets.only(top: 64),
            height: 90,
            decoration: BoxDecoration(
              color: Colors.grey.shade200,
              borderRadius: BorderRadius.circular(20),
              image: DecorationImage(
                image: AssetImage("assets/icon/app_icon.png"),
                fit: BoxFit.cover,
              ),
            ),
          ),

          const SizedBox(height: 128),
          CircularProgressIndicator(color: isDark ? Colors.white : Colors.black, strokeWidth: 4),
          const SizedBox(height: 90),
          SizedBox(height: 30),
          {% if REMOVE_WEMOB_BRANDING %}

          {% else %}
          Text("Created by", style: TextStyle(color: isDark ? Colors.white : Colors.black54)),
          SizedBox(width: 128, height: 48, child: Image.asset("assets/icon/wemob-logo-dark.png")),
          {% endif %}
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
        'This app is not active. Check wemob.io and \ncontact the app owner.\n\nsupport@wemob.io',
        textAlign: TextAlign.center,
        style: TextStyle(color: isDark ? Colors.white : Colors.black, fontWeight: FontWeight.w600),
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

    final controller = _controller;
    if (controller == null) {
      return _buildSplash(externalColor, isDark);
    }

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

    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, result) async {
        if (didPop) return;

        if (_controller != null) {
          final canGoBack = await _controller!.canGoBack();
          if (canGoBack) {
            _controller!.goBack();
          } else {
            SystemNavigator.pop();
          }
        }
      },
      child: Scaffold(
        backgroundColor: externalColor,
        body: AnnotatedRegion<SystemUiOverlayStyle>(
          value: overlayStyle,
          child: SafeArea(child: _buildBodyContent(externalColor, isDark)),
        ),
        bottomNavigationBar: _buildBottomNavigationBar(externalColor, isDark),
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

  Future<void> _loadNavigationItems() async {
    // Fetch navigation entries from backend so the bar reflects remote configuration.
    if (navigationConfigUrl.isEmpty) {
      return;
    }

    final controller = _controller;
    if (controller == null) {
      return;
    }

    try {
      final response =
          await http.get(Uri.parse(navigationConfigUrl)).timeout(const Duration(seconds: 10));

      if (response.statusCode != 200) {
        debugPrint('Navigation request failed with status: ${response.statusCode}');
        return;
      }

      dynamic decoded;
      try {
        decoded = jsonDecode(response.body);
      } catch (_) {
        debugPrint('Invalid navigation payload: ${response.body}');
        return;
      }

      final items = decoded is Map<String, dynamic> ? decoded['items'] : null;
      if (items is! List) {
        debugPrint('Navigation payload missing items array: ${response.body}');
        return;
      }

      final parsedItems = <NavItem>[];
      for (final item in items) {
        if (item is Map<String, dynamic>) {
          try {
            parsedItems.add(NavItem.fromJson(item));
          } catch (error) {
            debugPrint('Failed to parse navigation item: $error');
          }
        }
      }

      parsedItems.sort((a, b) => a.order.compareTo(b.order));

      if (!mounted) {
        return;
      }

      setState(() {
        _navItems = parsedItems;
        _selectedNavIndex = 0;
      });
    } catch (error) {
      debugPrint('Navigation request failed: $error');
    }
  }

  Widget? _buildBottomNavigationBar(Color background, bool isDark) {
    if (_navItems.isEmpty) {
      return null;
    }

    final activeColor = isDark ? Colors.white : Colors.black;
    final inactiveColor = activeColor.withOpacity(0.6);

    return BottomNavigationBar(
      backgroundColor: background,
      selectedItemColor: activeColor,
      unselectedItemColor: inactiveColor,
      type: BottomNavigationBarType.fixed,
      currentIndex: _selectedNavIndex,
      onTap: _handleNavigationTap,
      items: _navItems
          .map(
            (navItem) => BottomNavigationBarItem(
              icon: Icon(_iconForName(navItem.icon)),
              label: navItem.label,
            ),
          )
          .toList(),
    );
  }

  void _handleNavigationTap(int index) {
    if (index < 0 || index >= _navItems.length || _controller == null) {
      return;
    }

    final navItem = _navItems[index];
    final targetUri = _resolveNavUrl(navItem.url);

    setState(() {
      _selectedNavIndex = index;
    });

    _controller!.loadRequest(targetUri);
  }

  Uri _resolveNavUrl(String target) {
    final uri = Uri.tryParse(target);
    if (uri == null) {
      return Uri.parse(url);
    }

    if (uri.hasScheme || uri.hasAuthority) {
      return uri;
    }

    final baseUri = Uri.tryParse(url);
    if (baseUri == null) {
      return Uri.parse(target);
    }

    return baseUri.resolveUri(uri);
  }

  IconData _iconForName(String name) {
    switch (name) {
      case 'home':
        return Icons.home;
      case 'search':
        return Icons.search;
      case 'shopping_cart':
        return Icons.shopping_cart;
      case 'person':
        return Icons.person;
      case 'favorite':
        return Icons.favorite;
      case 'message':
        return Icons.message;
      case 'store':
        return Icons.store;
      default:
        return Icons.circle;
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
    WidgetsBinding.instance.removeObserver(this);
    _endSession();
    super.dispose();
  }
}
