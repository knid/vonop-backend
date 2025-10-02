import 'package:webview_flutter/webview_flutter.dart';

import 'constants.dart';
import 'utils.dart';

Future<NavigationDecision> handleNavigation(NavigationRequest request) async {
  final reqUri = Uri.parse(request.url);
  final scheme = reqUri.scheme.toLowerCase();
  final host = reqUri.host.toLowerCase();

  // If scheme is http/https, always allow navigation inside WebView
  if (scheme == 'http' || scheme == 'https') {
    // But if host is in externalDomains, open externally
    for (final extDomain in externalDomains) {
      if (host == extDomain || host.endsWith('.$extDomain')) {
        await openExternal(reqUri);
        return NavigationDecision.prevent;
      }
    }
    return NavigationDecision.navigate;
  }

  // If scheme is in externalSchemes, open externally
  if (externalSchemes.contains(scheme)) {
    await openExternal(reqUri);
    return NavigationDecision.prevent;
  }

  // All else: allow navigation (e.g., file://, data:, etc.)
  return NavigationDecision.navigate;
}
