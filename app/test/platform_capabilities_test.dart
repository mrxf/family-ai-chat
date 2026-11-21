import 'package:family_chat/platform/media/image_input.dart';
import 'package:family_chat/platform/platform_capabilities.dart';
import 'package:family_chat/platform/reminders/reminder_service.dart';
import 'package:family_chat/platform/share_intake/share_intake.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

ProviderContainer _containerFor(TargetPlatform platform) {
  final c = ProviderContainer(overrides: [
    platformCapabilitiesProvider.overrideWithValue(PlatformCapabilities.of(platform)),
  ]);
  addTearDown(c.dispose);
  return c;
}

void main() {
  test('capability table matches what each platform actually implements', () {
    final android = PlatformCapabilities.of(TargetPlatform.android);
    expect(android.isMobile, isTrue);
    expect(android.canTakePhoto, isTrue);
    expect(android.receivesSystemShare, isTrue);
    expect(android.localReminders, isTrue);
    expect(android.selfUpdateViaApk, isTrue);
    expect(android.deviceLabel, 'Android');

    final ios = PlatformCapabilities.of(TargetPlatform.iOS);
    expect(ios.receivesSystemShare, isFalse, reason: 'iOS has no share extension yet');
    expect(ios.localReminders, isTrue);
    expect(ios.selfUpdateViaApk, isFalse);

    final mac = PlatformCapabilities.of(TargetPlatform.macOS);
    expect(mac.isDesktop, isTrue);
    expect(mac.canTakePhoto, isFalse);
    expect(mac.localReminders, isFalse);
    expect(mac.nativeImageCompression, isTrue);
    expect(mac.deviceLabel, 'macOS');
    expect(mac.deviceNoun, '电脑');

    final win = PlatformCapabilities.of(TargetPlatform.windows);
    expect(win.isDesktop, isTrue);
    expect(win.nativeImageCompression, isFalse, reason: 'flutter_image_compress has no Windows implementation');
    expect(win.savedImagesPlace, contains('图片'));
  });

  test('desktop gets no-op platform services instead of missing-plugin errors', () async {
    final c = _containerFor(TargetPlatform.windows);
    final reminders = c.read(reminderServiceProvider);
    expect(reminders, isA<UnsupportedReminderService>());
    expect(await reminders.takePendingOpenSchedules(), isFalse);
    await reminders.syncReminders(const []);

    final intake = c.read(shareIntakeProvider);
    expect(intake, isA<NoShareIntake>());
    expect(await intake.takePending(), isNull);

    final input = c.read(imageInputProvider);
    expect(input.canTakePhoto, isFalse);
    expect(await input.takePhoto(), isNull);
  });

  test('phones talk to the native channels', () {
    final c = _containerFor(TargetPlatform.android);
    expect(c.read(reminderServiceProvider), isA<MethodChannelReminderService>());
    expect(c.read(shareIntakeProvider), isA<MethodChannelShareIntake>());
    expect(_containerFor(TargetPlatform.iOS).read(shareIntakeProvider), isA<NoShareIntake>());
  });

  test('temporary share copies are recognised for cleanup', () {
    expect(SharedPayload.isTemporaryCopy('/data/cache/shared_incoming/1.jpg'), isTrue);
    expect(SharedPayload.isTemporaryCopy('/Users/me/Pictures/cat.jpg'), isFalse);
  });
}
