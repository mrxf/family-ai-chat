import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../platform_capabilities.dart';

/// Content shared into 小暖 from another app (system share, WeChat "用其他应用打开", or text selection "问小暖").
@immutable
class SharedPayload {
  final String text;
  final List<String> imagePaths;
  final int totalImages;
  final int failedImages;

  const SharedPayload({
    this.text = '',
    this.imagePaths = const [],
    this.totalImages = 0,
    this.failedImages = 0,
  });

  bool get isEmpty => text.trim().isEmpty && imagePaths.isEmpty && failedImages == 0;

  static SharedPayload? fromMap(Map<Object?, Object?>? map) {
    if (map == null) return null;
    final text = (map['text'] as String?)?.trim() ?? '';
    final images = ((map['images'] as List?) ?? const []).whereType<String>().where((p) => p.isNotEmpty).toList(growable: false);
    final total = (map['totalImages'] as num?)?.toInt() ?? images.length;
    final failed = (map['failedImages'] as num?)?.toInt() ?? 0;
    final payload = SharedPayload(text: text, imagePaths: images, totalImages: total, failedImages: failed);
    return payload.isEmpty ? null : payload;
  }

  /// Temporary copies the native side made for this share; safe to delete
  /// once they have been read.
  static bool isTemporaryCopy(String path) => path.contains('shared_incoming');
}

/// Receives content other apps share into 小暖.
abstract interface class ShareIntake {
  /// Starts delivering shares that arrive while the app is running.
  void listen(void Function(SharedPayload payload) onShare);

  /// Stops delivering shares.
  void cancel();

  /// The share that cold-started the app, if any (consumed once).
  Future<SharedPayload?> takePending();
}

final shareIntakeProvider = Provider<ShareIntake>((ref) {
  final caps = ref.watch(platformCapabilitiesProvider);
  return caps.receivesSystemShare ? MethodChannelShareIntake() : const NoShareIntake();
});

class MethodChannelShareIntake implements ShareIntake {
  static const _channel = MethodChannel('com.nuan.chat/share');

  @override
  void listen(void Function(SharedPayload payload) onShare) {
    _channel.setMethodCallHandler((call) async {
      if (call.method == 'onShare' && call.arguments is Map) {
        final payload = SharedPayload.fromMap(call.arguments as Map<Object?, Object?>);
        if (payload != null) onShare(payload);
      }
    });
  }

  @override
  void cancel() => _channel.setMethodCallHandler(null);

  @override
  Future<SharedPayload?> takePending() async {
    try {
      return SharedPayload.fromMap(await _channel.invokeMapMethod<Object?, Object?>('takePendingShare'));
    } catch (_) {
      // Widget tests without a mocked share channel.
      return null;
    }
  }
}

/// Devices where nothing can be shared into the app yet.
class NoShareIntake implements ShareIntake {
  const NoShareIntake();

  @override
  void listen(void Function(SharedPayload payload) onShare) {}
  @override
  void cancel() {}
  @override
  Future<SharedPayload?> takePending() async => null;
}
