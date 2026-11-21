import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// What the current device can do. Features branch on these capabilities,
/// never on `Platform.isXxx`, so adding a platform means filling in one table.
@immutable
class PlatformCapabilities {
  const PlatformCapabilities({
    required this.platform,
    required this.canTakePhoto,
    required this.receivesSystemShare,
    required this.localReminders,
    required this.nativeImageCompression,
    required this.selfUpdateViaApk,
  });

  factory PlatformCapabilities.of(TargetPlatform platform) => switch (platform) {
        TargetPlatform.android => PlatformCapabilities(
            platform: platform,
            canTakePhoto: true,
            receivesSystemShare: true,
            localReminders: true,
            nativeImageCompression: true,
            selfUpdateViaApk: true,
          ),
        TargetPlatform.iOS => PlatformCapabilities(
            platform: platform,
            canTakePhoto: true,
            receivesSystemShare: false,
            localReminders: true,
            nativeImageCompression: true,
            selfUpdateViaApk: false,
          ),
        TargetPlatform.macOS => PlatformCapabilities(
            platform: platform,
            canTakePhoto: false,
            receivesSystemShare: false,
            localReminders: false,
            nativeImageCompression: true,
            selfUpdateViaApk: false,
          ),
        _ => PlatformCapabilities(
            platform: platform,
            canTakePhoto: false,
            receivesSystemShare: false,
            localReminders: false,
            nativeImageCompression: false,
            selfUpdateViaApk: false,
          ),
      };

  final TargetPlatform platform;

  /// A camera is reachable through the image picker (phones and tablets).
  final bool canTakePhoto;

  /// Other apps can share text/images into 小暖 (native share channel).
  final bool receivesSystemShare;

  /// Schedules ring as local OS notifications (native reminder channel).
  final bool localReminders;

  /// Images can be re-encoded natively before upload; otherwise the original
  /// file is sent as-is (still capped in size).
  final bool nativeImageCompression;

  /// New versions are installed by downloading an APK from the family server.
  final bool selfUpdateViaApk;

  bool get isDesktop => platform == TargetPlatform.macOS || platform == TargetPlatform.windows || platform == TargetPlatform.linux;
  bool get isMobile => !isDesktop;

  /// Keyboard shortcuts use ⌘ (Apple) rather than Ctrl.
  bool get usesCommandKey => platform == TargetPlatform.macOS || platform == TargetPlatform.iOS;

  /// Sent to the server on login so the sessions list can tell devices apart.
  String get deviceLabel => switch (platform) {
        TargetPlatform.android => 'Android',
        TargetPlatform.iOS => 'iOS',
        TargetPlatform.macOS => 'macOS',
        TargetPlatform.windows => 'Windows',
        TargetPlatform.linux => 'Linux',
        TargetPlatform.fuchsia => 'Fuchsia',
      };

  /// How the place saved pictures end up in is called on this device.
  String get savedImagesPlace => switch (platform) {
        TargetPlatform.macOS => '“照片”',
        TargetPlatform.windows => '“图片”文件夹',
        _ => '相册',
      };

  /// What to call the device in user-facing hints ("这台手机暂时不支持朗读").
  String get deviceNoun => isDesktop ? '电脑' : '手机';
}

/// Overridable in tests to exercise desktop or mobile behaviour on any host.
final platformCapabilitiesProvider = Provider<PlatformCapabilities>((_) => PlatformCapabilities.of(defaultTargetPlatform));
