import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

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
}

class IncomingShareNotifier extends Notifier<SharedPayload?> {
  static const _channel = MethodChannel('com.nuan.chat/share');

  @override
  SharedPayload? build() {
    _channel.setMethodCallHandler(_onMethodCall);
    ref.onDispose(() => _channel.setMethodCallHandler(null));
    unawaited(_pullInitial());
    return null;
  }

  Future<dynamic> _onMethodCall(MethodCall call) async {
    if (call.method == 'onShare' && call.arguments is Map) {
      final payload = SharedPayload.fromMap(call.arguments as Map<Object?, Object?>);
      if (payload != null) state = payload;
    }
  }

  Future<void> _pullInitial() async {
    try {
      final raw = await _channel.invokeMapMethod<Object?, Object?>('takePendingShare');
      final payload = SharedPayload.fromMap(raw);
      if (payload != null) state = payload;
    } on MissingPluginException {
      // Non-Android platform or widget tests without a mocked share channel.
    } catch (_) {}
  }

  /// Atomically takes and clears the pending share payload so it is consumed once.
  SharedPayload? take() {
    final current = state;
    if (current != null) state = null;
    return current;
  }

  @visibleForTesting
  void push(SharedPayload payload) {
    if (!payload.isEmpty) state = payload;
  }
}

final incomingShareProvider = NotifierProvider<IncomingShareNotifier, SharedPayload?>(IncomingShareNotifier.new);
