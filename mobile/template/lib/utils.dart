import 'package:flutter/widgets.dart';
import 'package:url_launcher/url_launcher.dart';

bool isDarkColor(Color color) {
  double brightness = (color.red * 299 + color.green * 587 + color.blue * 114) / 1000;
  return brightness < 128;
}

Future<void> openExternal(Uri uri) async {
  try {
    if (await canLaunchUrl(uri)) {
      await launchUrl(uri, mode: LaunchMode.externalApplication);
    } else {
      debugPrint('Could not launch $uri');
    }
  } catch (e) {
    debugPrint('Error launching $uri: $e');
  }
}
