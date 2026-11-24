import 'dart:async';
import 'dart:io';
import 'dart:math' as math;

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/models/models.dart';
import '../platform/reminders/reminder_service.dart';
import 'chat_state.dart';
import 'app_actions.dart';
import 'conversations.dart';
import 'pinned_components.dart';
import 'scheduled_tasks.dart';
import 'schedules.dart';
import 'session.dart';

export 'chat_state.dart';

/// One send / regenerate in flight. Owns its own stop flag so a stop tapped
/// before the server answered is honoured even if the user navigates away.
class _Op {
  bool stopRequested = false;
  bool abandoned = false;
}

class ChatController extends Notifier<ChatState> {
  final live = ValueNotifier<LiveText>(LiveText.empty);

  /// One-shot messages for the UI (snackbars).
  final notices = StreamController<String>.broadcast();

  int _attachGen = 0;
  CancelToken? _streamCancel;

  /// Bumps on every navigation (new chat / open / account change). Async work
  /// started under an older value must not touch the screen.
  int _nav = 0;

  /// Bumps on every local mutation (send / regenerate / edit). A background
  /// refresh started before a mutation is discarded.
  int _rev = 0;

  _Op? _op;
  final Map<String, List<PickedImage>> _failedImages = {};

  /// Model explicitly chosen by the user during this app session; carried across
  /// conversation switches and cleared on logout/account switch (and app restart).
  String? _sessionModelId;

  @override
  ChatState build() {
    bindAppActions(ref, live, () => state, (listener) => listenSelf(listener));
    final sessionNotifier = ref.read(sessionProvider.notifier);
    sessionNotifier.registerBeforeLogout(_onBeforeLogout);
    ref.onDispose(() {
      sessionNotifier.registerBeforeLogout(null);
      _detach();
      notices.close();
    });
    // Account switched or logged out: forget everything from the old session.
    ref.listen(sessionProvider.select((s) => s.api), (prev, next) {
      if (!identical(prev, next)) {
        _sessionModelId = null;
        _reset(const ChatState());
      }
    });
    return const ChatState();
  }

  Api get _api => ref.read(sessionProvider).api!;
  Api? get _apiOrNull => ref.read(sessionProvider).api;
  ConversationsNotifier get _convs => ref.read(conversationsProvider.notifier);

  Future<void> _cleanupPrivateState({Api? apiOverride, bool waitDelete = false}) async {
    if (!state.isPrivate) return;
    _op
      ?..stopRequested = true
      ..abandoned = true;
    final paths = <String>{
      for (final m in state.messages)
        for (final a in m.attachments)
          if (a.localPath != null && a.localPath!.isNotEmpty) a.localPath!,
      for (final list in _failedImages.values)
        for (final img in list)
          if (img.path.isNotEmpty) img.path,
    };
    for (final p in paths) {
      unawaited(File(p).delete().catchError((_) => File(p)));
    }
    final cid = state.conversationId;
    final api = apiOverride ?? _apiOrNull;
    if (cid != null && api != null) {
      final fut = api.deleteConversation(cid).catchError((_) {});
      if (waitDelete) await fut;
    }
  }

  Future<void> _onBeforeLogout(Api api) async {
    _sessionModelId = null;
    unawaited(ref.read(reminderServiceProvider).clearAll());
    if (!state.isPrivate) return;
    await _cleanupPrivateState(apiOverride: api, waitDelete: true);
    _nav++;
    _rev++;
    _detach();
    _failedImages.clear();
    live.value = LiveText.empty;
    state = const ChatState();
  }

  void _reset(ChatState next) {
    unawaited(_cleanupPrivateState());
    _nav++;
    _rev++;
    _detach();
    _failedImages.clear();
    live.value = LiveText.empty;
    state = next;
  }

  // ---------------- navigation ----------------

  void newChat({String? assistantId, bool isPrivate = false, QuotedComponent? quotedComponent}) {
    _reset(ChatState(
      assistantId: assistantId,
      modelId: _sessionModelId,
      isPrivate: isPrivate,
      think: state.think,
      epoch: state.epoch + 1,
      pendingQuote: quotedComponent,
    ));
  }

  /// Stages or clears a component quote in the composer without leaving the current screen.
  void setPendingQuote(QuotedComponent? quote) {
    state = state.copyWith(pendingQuote: quote);
  }

  /// Starts a fresh non-private chat with [quote] attached in the composer.
  /// If already on an empty home screen, stages the quote in place without resetting.
  void quoteComponentToNewChat(QuotedComponent quote) {
    if (state.conversationId == null && state.messages.isEmpty && !state.isPrivate && !state.loading) {
      state = state.copyWith(pendingQuote: quote);
      return;
    }
    newChat(quotedComponent: quote);
  }

  void clearHighlight() {
    if (state.highlightMessageId != null) {
      state = state.copyWith(highlightMessageId: null);
    }
  }

  void exitPrivateChat() {
    if (!state.isPrivate) return;
    final hadActivity = state.messages.isNotEmpty || state.conversationId != null || state.busy;
    newChat();
    if (hadActivity) notices.add('隐私对话已清除');
  }

  String? _convModeId(Conversation? c) {
    if (c == null) return null;
    if (c.autoMode && ref.read(sessionProvider).boot?.autoModel == true) return kAutoModelId;
    return c.modelId;
  }

  Future<void> open(String id, {String? focusMessageId}) async {
    if (state.conversationId == id && state.messages.isNotEmpty) {
      if (focusMessageId != null) {
        final idx = state.messages.indexWhere((m) => m.id == focusMessageId);
        if (idx >= 0) {
          state = state.copyWith(anchor: idx, highlightMessageId: focusMessageId, epoch: state.epoch + 1);
          return;
        }
      } else {
        return;
      }
    }
    final conv = ref.read(conversationsProvider).items.where((c) => c.id == id).firstOrNull;
    _reset(ChatState(
      conversationId: id,
      assistantId: conv?.assistantId,
      modelId: _sessionModelId ?? _convModeId(conv),
      resolvedModelId: conv?.modelId,
      isShared: conv?.isShared == true,
      think: state.think,
      loading: true,
      epoch: state.epoch + 1,
      highlightMessageId: focusMessageId,
    ));
    final nav = _nav;
    final cached = await ref.read(storageProvider).readCache('conv_$id');
    if (nav != _nav) return;
    if (cached is List && cached.isNotEmpty) {
      final msgs = cached.map((e) => Message.fromJson((e as Map).cast())).toList();
      final focusIdx = focusMessageId == null ? -1 : msgs.indexWhere((m) => m.id == focusMessageId);
      if (focusMessageId == null || focusIdx >= 0) {
        state = state.copyWith(
          messages: msgs,
          anchor: focusIdx >= 0 ? focusIdx : msgs.length,
          loading: false,
        );
      }
    }
    await _reload(id, focusMessageId: focusMessageId);
  }

  Future<void> _reload(String id, {String? focusMessageId}) async {
    final api = _apiOrNull;
    if (api == null) return;
    final nav = _nav, rev = _rev;
    try {
      final d = await api.conversation(id, focusMessageId: focusMessageId);
      if (nav != _nav || rev != _rev || state.conversationId != id) return;
      final active = d.activeMessageId.isEmpty ? null : d.activeMessageId;
      final merged = focusMessageId != null ? d.messages : _mergeTail(state.messages, d.messages);
      final focusIdx = focusMessageId == null ? -1 : merged.indexWhere((m) => m.id == focusMessageId);
      state = state.copyWith(
        messages: merged,
        anchor: focusIdx >= 0
            ? focusIdx
            : (active != null ? _lastUserIndex(merged) : _keepAnchor(merged)),
        assistantId: d.conversation.assistantId,
        modelId: _sessionModelId ?? _convModeId(d.conversation),
        resolvedModelId: d.conversation.modelId,
        isShared: d.conversation.isShared,
        hasMore: merged.length > d.messages.length ? state.hasMore : d.hasMore,
        loading: false,
        activeId: active,
        activeUserId: d.activeUserId,
        activeUserName: d.activeUserName,
        lastSyncUpdatedAt: d.conversation.updatedAt,
        epoch: focusIdx >= 0 ? state.epoch + 1 : state.epoch,
      );
      _convs.replaceConversation(d.conversation);
      _persist();
      if (active != null) {
        unawaited(_attach(active));
      } else {
        _detach();
      }
    } on ApiError catch (e) {
      if (nav != _nav || state.conversationId != id) return;
      state = state.copyWith(loading: false);
      if (e.status == 404) {
        notices.add(state.isPrivate ? '隐私对话已过期自动清除' : '这个对话已经被删除了');
        newChat();
      } else if (state.messages.isEmpty) {
        notices.add(e.message);
      }
    }
  }

  /// Keeps older pages the user already loaded when the latest page is refreshed.
  List<Message> _mergeTail(List<Message> current, List<Message> latest) {
    if (latest.isEmpty || current.isEmpty) return latest;
    final cut = current.indexWhere((m) => m.id == latest.first.id);
    if (cut <= 0) return latest;
    return [...current.take(cut), ...latest];
  }

  Future<void> loadOlder() async {
    final id = state.conversationId;
    final api = _apiOrNull;
    if (api == null || id == null || !state.hasMore || state.loadingOlder || state.messages.isEmpty) return;
    final nav = _nav;
    state = state.copyWith(loadingOlder: true);
    try {
      final (older, more) = await api.olderMessages(id, state.messages.first.id);
      if (nav != _nav) return;
      state = state.copyWith(messages: [...older, ...state.messages], anchor: state.anchor + older.length, hasMore: more, loadingOlder: false);
    } catch (_) {
      if (nav == _nav) state = state.copyWith(loadingOlder: false);
    }
  }

  /// Applies an incremental sync snapshot for an active shared conversation.
  void applySharedSync(ConversationSyncResult sync) {
    final cid = state.conversationId;
    if (cid == null || sync.conversation?.id != cid) return;
    if (state.activeId?.startsWith('local-') == true) return;
    final active = sync.activeMessageId.isEmpty ? null : sync.activeMessageId;
    if (sync.changed) {
      final merged = _mergeTail(state.messages, sync.messages);
      state = state.copyWith(
        messages: merged,
        anchor: _keepAnchor(merged),
        hasMore: merged.length > sync.messages.length ? state.hasMore : sync.hasMore,
        isShared: sync.conversation?.isShared ?? state.isShared,
        activeId: active,
        activeUserId: sync.activeUserId,
        activeUserName: sync.activeUserName,
        lastSyncUpdatedAt: sync.updatedAt,
      );
      if (sync.conversation != null) _convs.replaceConversation(sync.conversation!);
      _persist();
    } else if (state.activeId != active || state.activeUserId != sync.activeUserId) {
      state = state.copyWith(activeId: active, activeUserId: sync.activeUserId, activeUserName: sync.activeUserName);
    }
    if (active != null && active != state.activeId) unawaited(_attach(active));
  }

  /// Removes a deleted message from local state immediately after server deletion.
  void removeMessageLocally(String msgId) {
    final next = state.messages.where((m) => m.id != msgId).toList();
    state = state.copyWith(messages: next, anchor: _keepAnchor(next));
    _persist();
  }

  /// Called when the app returns to the foreground: the SSE connection may
  /// have died while backgrounded, so reattach (the server replays a snapshot).
  void onResume() {
    final id = state.activeId;
    if (id != null && !id.startsWith('local-')) {
      unawaited(_attach(id));
    } else if (state.conversationId != null && state.activeId == null) {
      unawaited(_reload(state.conversationId!));
    }
  }

  // ---------------- options ----------------

  void setModel(String? modelId) {
    _sessionModelId = modelId;
    state = state.copyWith(
      modelId: modelId,
      resolvedModelId: modelId == kAutoModelId ? state.resolvedModelId : modelId,
    );
  }
  void setThink(bool v) => state = state.copyWith(think: v);

  // ---------------- sending ----------------

  Future<void> send(String text, {List<PickedImage> images = const [], List<Attachment> keepAttachments = const [], String? editOf}) async {
    text = text.trim();
    if ((text.isEmpty && images.isEmpty && keepAttachments.isEmpty) || state.busy || state.loading) return;
    // Freeze everything the request depends on before the first await.
    final api = _api;
    final nav = _nav;
    final convId = state.conversationId;
    final assistantId = convId == null ? state.assistantId : null;
    final modelId = state.modelId;
    final think = state.think;
    final isPrivate = state.isPrivate;
    final quoted = editOf == null ? state.pendingQuote : null;
    final original = (messages: state.messages, anchor: state.anchor);
    final op = _op = _Op();
    _rev++;

    final ts = DateTime.now().microsecondsSinceEpoch;
    final localUser = Message(
      id: 'local-u-$ts',
      conversationId: convId ?? '',
      role: 'user',
      content: text,
      quotedComponent: quoted,
      status: MsgStatus.sending,
      attachments: [
        ...keepAttachments,
        for (final img in images) Attachment(fileId: '', mime: 'image/jpeg', width: img.width, height: img.height, localPath: img.path),
      ],
    );
    final localAsst = Message(id: 'local-a-$ts', conversationId: convId ?? '', role: 'assistant', status: MsgStatus.streaming);

    var base = state.messages;
    if (editOf != null) {
      final i = base.indexWhere((m) => m.id == editOf);
      if (i >= 0) base = base.sublist(0, i);
    }
    live.value = LiveText('', '', activity: images.isNotEmpty ? '正在上传图片…' : '正在发送…', activitySince: DateTime.now());
    state = state.copyWith(messages: [...base, localUser, localAsst], anchor: base.length, activeId: localAsst.id, pendingQuote: null);
    bool current() => nav == _nav && state.activeId == localAsst.id;

    try {
      final uploaded = <Attachment>[...keepAttachments];
      for (final img in images) {
        if (isPrivate && op.abandoned) return;
        final a = await api.upload(img.bytes, 'photo.jpg', isPrivate: isPrivate);
        uploaded.add(Attachment(fileId: a.fileId, mime: a.mime, width: a.width, height: a.height, localPath: img.path));
      }
      if (isPrivate && op.abandoned) return;
      final res = await api.send(
        conversationId: convId,
        assistantId: assistantId,
        modelId: modelId,
        content: text,
        attachments: uploaded,
        quotedComponent: quoted,
        editOf: editOf,
        think: think,
        isPrivate: isPrivate,
      );
      if (op.stopRequested) unawaited(api.cancel(res.assistant.id).catchError((_) {}));
      final privConv = isPrivate || res.conversation.isPrivate;
      if (!privConv && identical(_apiOrNull, api)) {
        _convs.upsertTop(res.conversation.copyWith(updatedAt: DateTime.now().millisecondsSinceEpoch));
      }
      if (!current()) {
        if (privConv && convId == null) {
          unawaited(api.deleteConversation(res.conversation.id).catchError((_) {}));
        }
        return; // user navigated away; normal chats remain safe on the server
      }
      final serverUser = res.userMessage!;
      // Keep local file paths so images don't flash while switching to network URLs.
      final mergedUser = serverUser.copyWith(attachments: [
        for (var i = 0; i < serverUser.attachments.length; i++)
          i < uploaded.length
              ? Attachment(
                  fileId: serverUser.attachments[i].fileId,
                  mime: serverUser.attachments[i].mime,
                  width: serverUser.attachments[i].width,
                  height: serverUser.attachments[i].height,
                  localPath: uploaded[i].localPath)
              : serverUser.attachments[i],
      ]);
      state = state.copyWith(
        conversationId: res.conversation.id,
        isPrivate: privConv,
        resolvedModelId: res.assistant.modelId ?? res.conversation.modelId ?? state.resolvedModelId,
        messages: [
          for (final m in state.messages)
            if (m.id == localUser.id) mergedUser else if (m.id == localAsst.id) res.assistant else m,
        ],
        activeId: res.assistant.id,
      );
      unawaited(_attach(res.assistant.id));
    } catch (e) {
      final err = ApiError.from(e);
      if (!current()) return;
      if (editOf != null) {
        // Nothing changed on the server: put the original history back.
        state = state.copyWith(messages: original.messages, anchor: original.anchor, activeId: null);
      } else {
        _failedImages[localUser.id] = images;
        state = state.copyWith(
          messages: [
            for (final m in state.messages)
              if (m.id == localUser.id) m.copyWith(status: MsgStatus.error, error: err.message) else if (m.id != localAsst.id) m,
          ],
          activeId: null,
        );
      }
      notices.add(err.message);
    } finally {
      if (identical(_op, op)) _op = null;
    }
  }

  /// Retry a user message that failed to send.
  Future<void> retryFailed(Message m) async {
    if (state.busy || state.loading || !state.messages.any((x) => x.id == m.id)) return;
    final images = _failedImages.remove(m.id) ?? const [];
    state = state.copyWith(messages: state.messages.where((x) => x.id != m.id).toList(), anchor: state.messages.length - 1);
    await send(m.content, images: images, keepAttachments: m.attachments.where((a) => a.fileId.isNotEmpty).toList());
  }

  Future<void> editAndResend(Message userMsg, String newText) async {
    if (state.busy || state.loading) return;
    if (userMsg.id.startsWith('local-')) {
      final images = _failedImages.remove(userMsg.id) ?? const [];
      state = state.copyWith(messages: state.messages.where((x) => x.id != userMsg.id).toList(), anchor: state.messages.length - 1);
      return send(newText, images: images);
    }
    await send(newText, keepAttachments: userMsg.attachments, editOf: userMsg.id);
  }

  Future<void> regenerate({String? modelId}) async {
    final convId = state.conversationId;
    if (convId == null || state.busy || state.loading || state.messages.isEmpty) return;
    final api = _api;
    final nav = _nav;
    final original = (messages: state.messages, anchor: state.anchor, modelId: state.modelId);
    final prevSessionModelId = _sessionModelId;
    final op = _op = _Op();
    _rev++;
    var base = state.messages;
    if (base.last.role == 'assistant') base = base.sublist(0, base.length - 1);
    final placeholder = Message(id: 'local-a-${DateTime.now().microsecondsSinceEpoch}', conversationId: convId, role: 'assistant', status: MsgStatus.streaming);
    live.value = LiveText('', '', activity: '正在连接…', activitySince: DateTime.now());
    if (modelId != null) {
      _sessionModelId = modelId;
      state = state.copyWith(modelId: modelId);
    }
    final think = state.think;
    final useModel = state.modelId;
    state = state.copyWith(messages: [...base, placeholder], anchor: _lastUserIndex(base), activeId: placeholder.id);
    bool current() => nav == _nav && state.activeId == placeholder.id;
    try {
      final res = await api.regenerate(convId, modelId: useModel, think: think);
      if (op.stopRequested) unawaited(api.cancel(res.assistant.id).catchError((_) {}));
      if (!current()) return;
      state = state.copyWith(messages: [for (final m in state.messages) m.id == placeholder.id ? res.assistant : m], activeId: res.assistant.id);
      unawaited(_attach(res.assistant.id));
    } catch (e) {
      if (!current()) return;
      _sessionModelId = prevSessionModelId;
      state = state.copyWith(messages: original.messages, anchor: original.anchor, modelId: original.modelId, activeId: null);
      notices.add(ApiError.from(e).message);
    } finally {
      if (identical(_op, op)) _op = null;
    }
  }

  Future<void> stop() async {
    final id = state.activeId;
    if (id == null) return;
    if (id.startsWith('local-')) {
      _op?.stopRequested = true;
      return;
    }
    try {
      await _api.cancel(id);
    } catch (_) {}
  }

  // ---------------- streaming ----------------

  void _detach() {
    _attachGen++;
    _streamCancel?.cancel();
    _streamCancel = null;
  }

  Future<void> _attach(String id) async {
    _detach();
    final gen = _attachGen;
    final api = _api;
    var backoff = const Duration(milliseconds: 600);
    while (gen == _attachGen) {
      final cancel = CancelToken();
      _streamCancel = cancel;
      try {
        await for (final ev in api.stream(id, cancel)) {
          if (gen != _attachGen) return;
          backoff = const Duration(milliseconds: 600);
          switch (ev.event) {
            case 'snapshot':
              final snapModel = '${ev.data['model_id'] ?? ''}';
              if (snapModel.isNotEmpty && snapModel != state.resolvedModelId) {
                state = state.copyWith(
                  resolvedModelId: snapModel,
                  messages: [for (final m in state.messages) m.id == id ? m.copyWith(modelId: snapModel) : m],
                );
              }
              live.value = live.value.copyWith(
                content: '${ev.data['content'] ?? ''}',
                reasoning: '${ev.data['reasoning'] ?? ''}',
                activity: '${ev.data['activity'] ?? ''}',
                activityAt: (ev.data['activity_at'] as num?)?.toInt(),
                steps: _steps(ev.data['steps']),
                attachments: _attachments(ev.data['attachments']),
                sources: _sources(ev.data['sources']),
                components: _components(ev.data['components']),
              );
            case 'steps':
              live.value = live.value.copyWith(steps: _steps(ev.data['steps']));
            case 'sources':
              live.value = live.value.copyWith(sources: _sources(ev.data['sources']));
            case 'components':
              live.value = live.value.copyWith(components: _components(ev.data['components']));
            case 'model':
              final mid = '${ev.data['model_id'] ?? ''}';
              if (mid.isNotEmpty) {
                state = state.copyWith(
                  resolvedModelId: mid,
                  messages: [for (final m in state.messages) m.id == id ? m.copyWith(modelId: mid) : m],
                );
                if (!state.isPrivate && ev.data['conversation_id'] != null) {
                  _convs.patchLocal('${ev.data['conversation_id']}', (c) => c.copyWith(modelId: mid));
                }
              }
            case 'delta':
              final v = live.value;
              live.value = v.copyWith(content: '${v.content}${ev.data['content'] ?? ''}', reasoning: '${v.reasoning}${ev.data['reasoning'] ?? ''}');
            case 'activity':
              live.value = live.value.copyWith(activity: '${ev.data['text'] ?? ''}', activityAt: (ev.data['at'] as num?)?.toInt());
            case 'attachments':
              live.value = live.value.copyWith(attachments: _attachments(ev.data['attachments']));
            case 'title':
              if (!state.isPrivate) {
                final cid = '${ev.data['conversation_id']}';
                final title = '${ev.data['title']}';
                _convs.patchLocal(cid, (c) => c.copyWith(title: title));
              }
            case 'done':
              final m = ev.data['message'];
              _finish(id, m is Map ? Message.fromJson(m.cast()) : null);
              return;
          }
        }
      } on ApiError catch (e) {
        if (gen != _attachGen || e.code == 'cancelled') return;
        if (e.status == 404 || e.status == 401) {
          _finish(id, null);
          return;
        }
      }
      if (gen != _attachGen) return;
      await Future.delayed(backoff);
      backoff = Duration(milliseconds: math.min(backoff.inMilliseconds * 2, 8000));
    }
  }

  static List<Attachment> _attachments(dynamic raw) =>
      raw is List ? [for (final e in raw) if (e is Map) Attachment.fromJson(e.cast())] : const [];

  static List<RunStep> _steps(dynamic raw) =>
      raw is List ? [for (final e in raw) if (e is Map) RunStep.fromJson(e.cast())] : const [];

  static List<WebSource> _sources(dynamic raw) =>
      raw is List ? [for (final e in raw) if (e is Map) WebSource.fromJson(e.cast())] : const [];

  static List<MessageComponent> _components(dynamic raw) =>
      raw is List ? [for (final e in raw) if (e is Map) MessageComponent.fromJson(e.cast())] : const [];

  /// Optimistically updates an interactive component's state (e.g. Todo check,
  /// Quiz choice, AskUser submission) and persists it to the server.
  Future<void> updateComponentState(String messageId, String compId, Map<String, dynamic> nextState) async {
    if (messageId.isEmpty || compId.isEmpty) return;
    if (state.activeId == messageId) {
      final cur = live.value;
      live.value = cur.copyWith(
        components: [
          for (final c in cur.components) c.id == compId ? c.copyWith(state: nextState) : c,
        ],
      );
    }
    state = state.copyWith(
      messages: [
        for (final m in state.messages)
          if (m.id == messageId)
            m.copyWith(
              components: [
                for (final c in m.components) c.id == compId ? c.copyWith(state: nextState) : c,
              ],
            )
          else
            m,
      ],
    );
    _persist();
    ref.read(pinnedComponentsProvider.notifier).syncComponentStateFromChat(messageId, compId, nextState);
    final api = _apiOrNull;
    if (api != null && !messageId.startsWith('local-')) {
      try {
        await api.updateMessageComponentState(messageId, compId, nextState);
      } catch (_) {}
    }
  }

  /// Updates an interactive component's content data in place and persists it.
  Future<String?> updateComponentData(String messageId, String compId, Map<String, dynamic> nextData) async {
    if (messageId.isEmpty || compId.isEmpty) return '无效卡片';
    final api = _apiOrNull;
    if (api == null) return '尚未登录';
    syncComponentDataFromPin(messageId, compId, nextData);
    ref.read(pinnedComponentsProvider.notifier).syncComponentDataFromChat(messageId, compId, nextData);
    if (messageId.startsWith('local-')) return null;
    try {
      final saved = await api.updateMessageComponent(messageId, compId, data: nextData);
      syncComponentDataFromPin(messageId, compId, saved.data);
      ref.read(pinnedComponentsProvider.notifier).syncComponentDataFromChat(messageId, compId, saved.data);
      return null;
    } catch (e) {
      return ApiError.from(e).message;
    }
  }

  /// Reflects a component data edit performed on the home board inside the open conversation.
  void syncComponentDataFromPin(
    String messageId,
    String compId,
    Map<String, dynamic> nextData,
  ) =>
      _patchMessage(messageId, (m) => m.copyWith(components: [for (final c in m.components) c.id == compId ? c.copyWith(data: nextData) : c]));

  /// Removes a deleted component from the currently open conversation immediately.
  void removeComponentLocal(String messageId, String compId) =>
      _patchMessage(messageId, (m) => m.copyWith(components: m.components.where((c) => c.id != compId).toList()));

  /// Reflects a component state edit performed on the home board inside the open conversation.
  void syncComponentStateFromPin(String messageId, String compId, Map<String, dynamic> nextState) =>
      _patchMessage(messageId, (m) => m.copyWith(components: [for (final c in m.components) c.id == compId ? c.copyWith(state: nextState) : c]));

  void _patchMessage(String messageId, Message Function(Message) transform) {
    if (!state.messages.any((m) => m.id == messageId)) return;
    state = state.copyWith(messages: [for (final m in state.messages) m.id == messageId ? transform(m) : m]);
    _persist();
  }

  /// Persists an `ask_user` card selection and sends the formatted answer as the next user message.
  Future<void> submitAskUserAnswer(
    String messageId,
    String compId,
    Map<String, dynamic> nextState,
    String replyText,
  ) async {
    await updateComponentState(messageId, compId, nextState);
    if (replyText.trim().isNotEmpty && !state.busy && !state.loading) {
      await send(replyText);
    }
  }

  void _finish(String expectedId, Message? finalMsg) {
    if (state.activeId != expectedId) return;
    final v = live.value;
    final msgs = [
      for (final m in state.messages)
        if (m.id == expectedId)
          finalMsg ??
              m.copyWith(
                content: v.content,
                reasoning: v.reasoning,
                attachments: v.attachments,
                sources: v.sources,
                components: v.components,
                status: MsgStatus.interrupted,
              )
        else
          m,
    ];
    state = state.copyWith(
      messages: msgs,
      resolvedModelId: finalMsg?.modelId ?? state.resolvedModelId,
      activeId: null,
    );
    _streamCancel = null;
    final cid = state.conversationId;
    if (!state.isPrivate && cid != null) _convs.touch(cid);
    // A limited user's "pictures left today" changes with each drawing.
    final boot = ref.read(sessionProvider).boot;
    if ((finalMsg?.attachments.isNotEmpty ?? false) && boot != null && boot.imagesLeft >= 0) {
      ref.read(sessionProvider.notifier).refresh();
    }
    if (!state.isPrivate && v.steps.any((s) => s.kind == 'schedule')) {
      unawaited(syncSchedulesAfterChat(ref));
    }
    if (!state.isPrivate && v.steps.any((s) => s.kind == 'automation')) {
      unawaited(syncScheduledTasksAfterChat(ref));
    }
    if (!state.isPrivate && v.steps.any((s) => s.kind == 'component' || s.kind == 'app')) {
      unawaited(ref.read(pinnedComponentsProvider.notifier).refresh());
      // Refresh the open conversation if an earlier message's component was updated/deleted in place.
      final updatedInPlace = v.steps.any((s) => s.kind == 'component' && (s.title.contains('更新') || s.title.contains('删除')));
      if (updatedInPlace && cid != null) {
        unawaited(_reload(cid));
      }
    }
    _persist();
  }

  void _persist() {
    if (state.isPrivate) return;
    final id = state.conversationId;
    if (id == null) return;
    final keep = state.messages.where((m) => !m.id.startsWith('local-')).toList();
    final tail = keep.length > 60 ? keep.sublist(keep.length - 60) : keep;
    ref.read(storageProvider).writeCache('conv_$id', tail.map((m) => m.toJson()).toList());
  }

  /// Keeps the current scroll anchor if its message is still there, so a
  /// background refresh doesn't make the view jump.
  int _keepAnchor(List<Message> msgs) {
    final cur = state.anchor < state.messages.length ? state.messages[state.anchor].id : null;
    if (cur == null) return msgs.length;
    final i = msgs.indexWhere((m) => m.id == cur);
    return i < 0 ? msgs.length : i;
  }

  int _lastUserIndex(List<Message> msgs) {
    for (var i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i].isUser) return i;
    }
    return msgs.length;
  }
}

final chatProvider = NotifierProvider<ChatController, ChatState>(ChatController.new);
