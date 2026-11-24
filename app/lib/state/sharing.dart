import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/models/models.dart';
import 'chat.dart';
import 'conversations.dart';
import 'session.dart';

/// Looks up the active [Conversation] object for the currently open chat, if any.
final currentConversationProvider = Provider<Conversation?>((ref) {
  final cid = ref.watch(chatProvider.select((s) => s.conversationId));
  if (cid == null) return null;
  final items = ref.watch(conversationsProvider.select((s) => s.items));
  return items.where((c) => c.id == cid).firstOrNull;
});

/// Active household members (excluding current user) for the family-share picker.
final familyMembersProvider = FutureProvider.autoDispose<List<FamilyMember>>((ref) async {
  final api = ref.watch(sessionProvider.select((s) => s.api));
  if (api == null) return const [];
  return api.familyMembers();
});

/// Conversations owned by the current user that are currently shared with family.
final ownedSharedConversationsProvider = FutureProvider.autoDispose<List<Conversation>>((ref) async {
  final api = ref.watch(sessionProvider.select((s) => s.api));
  if (api == null) return const [];
  return api.ownedSharedConversations();
});

/// Active short-code shares created by the current user.
final userSharesProvider = FutureProvider.autoDispose<List<ConversationShare>>((ref) async {
  final api = ref.watch(sessionProvider.select((s) => s.api));
  if (api == null) return const [];
  return api.userShares();
});

/// Lightweight periodic poller active only while viewing a family-shared conversation.
/// Keeps messages, live components and collaborator busy state up to date across devices.
class SharedSyncController extends Notifier<void> {
  Timer? _timer;
  bool _inFlight = false;

  @override
  void build() {
    ref.onDispose(_stop);
    final conv = ref.watch(currentConversationProvider);
    final api = ref.watch(sessionProvider.select((s) => s.api));
    _stop();
    if (api != null && conv != null && conv.isShared) {
      _timer = Timer.periodic(const Duration(seconds: 3), (_) => unawaited(_poll(conv.id)));
    }
  }

  void _stop() {
    _timer?.cancel();
    _timer = null;
  }

  Future<void> _poll(String convId) async {
    if (_inFlight || !ref.mounted) return;
    final api = ref.read(sessionProvider).api;
    final chat = ref.read(chatProvider);
    if (api == null || chat.conversationId != convId) return;
    // Skip polling while our own local SSE stream is actively running.
    final myId = ref.read(sessionProvider).user?.id ?? '';
    if (chat.busy && (chat.activeUserId.isEmpty || chat.activeUserId == myId)) return;

    _inFlight = true;
    try {
      final res = await api.syncConversation(convId, since: chat.lastSyncUpdatedAt);
      if (!ref.mounted) return;
      if (ref.read(chatProvider).conversationId == convId) {
        ref.read(chatProvider.notifier).applySharedSync(res);
      }
    } on ApiError catch (e) {
      if (e.status == 404 && ref.mounted && ref.read(chatProvider).conversationId == convId) {
        ref.read(conversationsProvider.notifier).refresh();
        ref.read(chatProvider.notifier).notices.add('这个共享对话已被收回或解散');
        ref.read(chatProvider.notifier).newChat();
      }
    } catch (_) {
      // Transient network hiccup; retry on next tick.
    } finally {
      _inFlight = false;
    }
  }
}

final sharedSyncProvider = NotifierProvider<SharedSyncController, void>(SharedSyncController.new);
