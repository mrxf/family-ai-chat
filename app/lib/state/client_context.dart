import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../platform/platform_capabilities.dart';
import 'pinned_components.dart';
import 'session.dart';

/// Captures fresh device preferences at send time, including changes made outside chat.
Map<String, dynamic> buildClientContext(Ref ref) {
  final settings = ref.read(settingsProvider);
  final caps = ref.read(platformCapabilitiesProvider);
  return {
    'device_id': ref.read(deviceIdProvider),
    'device_kind': caps.isDesktop ? 'desktop' : 'mobile',
    'settings': {'font_scale': settings.fontScale, 'theme': settings.themeMode.name},
    'capabilities': {'font_scale': true, 'theme': true, 'suggestions': true},
    'suggestions_pinned': ref.read(pinnedComponentsProvider).suggestionsPinned,
  };
}
