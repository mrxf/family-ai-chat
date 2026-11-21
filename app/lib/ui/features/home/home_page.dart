import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../app/navigation.dart';
import '../../../platform/reminders/reminder_service.dart';
import '../../../platform/speech/speaker.dart';
import '../../../state/chat.dart';
import '../../../state/incoming_share.dart';
import '../../../state/schedules.dart';
import '../../../state/session.dart';
import '../../adaptive/app_route.dart';
import '../../adaptive/breakpoints.dart';
import '../../desktop/desktop_home_layout.dart';
import '../../mobile/mobile_home_layout.dart';
import '../../widgets/base_widgets.dart';
import '../chat/composer/composer.dart';
import '../schedules/schedules_page.dart';
import '../settings/update.dart';

/// The signed-in home screen.
///
/// Owns everything that outlives a layout switch — the composer draft,
/// notices, content shared from other apps, reminder taps, update checks and
/// app lifecycle — and delegates the layout to [MobileHomeLayout] or
/// [DesktopHomeLayout] depending on the window width.
class HomePage extends ConsumerStatefulWidget {
  const HomePage({super.key});

  @override
  ConsumerState<HomePage> createState() => _HomePageState();
}

class _HomePageState extends ConsumerState<HomePage> with WidgetsBindingObserver {
  final _composerKey = GlobalKey<ComposerState>();
  StreamSubscription<String>? _notices;
  ProviderSubscription<SharedPayload?>? _shareSub;
  bool _shareScheduled = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _notices = ref.read(chatProvider.notifier).notices.stream.listen((m) {
      if (mounted) showToast(context, m);
    });
    _shareSub = ref.listenManual<SharedPayload?>(
      incomingShareProvider,
      (_, next) {
        if (next != null) _scheduleConsumeShare();
      },
      fireImmediately: true,
    );
    Future.delayed(const Duration(seconds: 2), () {
      if (mounted) checkForUpdate(context, ref, silent: true);
    });
    final reminders = ref.read(reminderServiceProvider);
    reminders.attach(onOpenSchedules: _openSchedulesFromNotification);
    Future.microtask(() async {
      if (await reminders.takePendingOpenSchedules()) {
        _openSchedulesFromNotification();
      }
    });
  }

  void _openSchedulesFromNotification() {
    if (!mounted || ref.read(sessionProvider).boot == null) return;
    navigatorKey.currentState?.popUntil((r) => r.isFirst);
    navigatorKey.currentState?.push(appRoute((_) => const SchedulesPage()));
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _notices?.cancel();
    _shareSub?.close();
    super.dispose();
  }

  void _scheduleConsumeShare() {
    if (_shareScheduled) return;
    _shareScheduled = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _shareScheduled = false;
      _consumeIncomingShare();
    });
    WidgetsBinding.instance.ensureVisualUpdate();
  }

  void _consumeIncomingShare() {
    if (!mounted || ref.read(sessionProvider).boot == null) return;
    final payload = ref.read(incomingShareProvider.notifier).take();
    if (payload == null || payload.isEmpty) return;
    navigatorKey.currentState?.popUntil((r) => r.isFirst);
    final chat = ref.read(chatProvider);
    final hasActivity = chat.conversationId != null || chat.messages.isNotEmpty || chat.loading || chat.busy;
    if (hasActivity) {
      _composerKey.currentState?.clearDraft();
      if (chat.isPrivate) {
        ref.read(chatProvider.notifier).exitPrivateChat();
      } else {
        ref.read(chatProvider.notifier).newChat();
      }
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      _composerKey.currentState?.applyShared(
        text: payload.text,
        imagePaths: payload.imagePaths,
        totalImages: payload.totalImages,
        failedImages: payload.failedImages,
      );
    });
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      ref.read(chatProvider.notifier).onResume();
      ref.read(sessionProvider.notifier).refresh();
      ref.invalidate(schedulesProvider);
    } else if (state == AppLifecycleState.paused) {
      Speaker.instance.stop();
    }
  }

  void _unfocusComposer() {
    _composerKey.currentState?.unfocus();
    FocusManager.instance.primaryFocus?.unfocus();
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(chatProvider.select((s) => s.epoch), (_, _) => _unfocusComposer());
    // Eagerly keep schedules & local notifications synced while logged in.
    ref.watch(schedulesProvider.select((_) => 0));
    // Can be null for a frame while logging out (the page is animating away).
    if (ref.watch(sessionProvider.select((s) => s.boot == null))) return const Scaffold();
    if (ref.read(incomingShareProvider) != null) _scheduleConsumeShare();
    // The composer keeps its draft when the window is resized across the
    // breakpoint: both layouts host it under the same GlobalKey.
    return context.windowSize.isExpanded
        ? DesktopHomeLayout(composerKey: _composerKey)
        : MobileHomeLayout(composerKey: _composerKey, onUnfocus: _unfocusComposer);
  }
}
