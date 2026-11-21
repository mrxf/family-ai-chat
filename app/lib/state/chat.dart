import 'dart:async';
import 'dart:io';
import 'dart:math' as math;

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/models/models.dart';
import '../platform/reminders/reminder_bridge.dart';
import 'conversations.dart';
import 'schedules.dart';
import 'session.dart';

/// Live text of the generation in progress. Only the streaming bubble listens
/// to this, so tokens never rebuild the whole message list.
enum LivePhase { waiting, reasoning, answering }

@immutable
class RunStep {
  final String kind, title, detail, status;
  final DateTime? startedAt;

  const RunStep({
    required this.kind,
    required this.title,
    this.detail = '',
    this.status = 'done',
    this.startedAt,
  });

  bool get running => status == 'running';
  bool get isError => status == 'error';

  factory RunStep.fromJson(Map<String, dynamic> j) {
    final ts = (j['started_at'] as num?)?.toInt() ?? 0;
    return RunStep(
      kind: '${j['kind'] ?? ''}',
      title: '${j['title'] ?? ''}',
      detail: '${j['detail'] ?? ''}',
      status: '${j['status'] ?? 'done'}',
      startedAt: ts > 0 ? DateTime.fromMillisecondsSinceEpoch(ts) : null,
    );
  }
}

@immutable
class LiveText {
  final String content, reasoning;

  /// What a tool is doing right now ("正在画图…"), empty when idle.
  final String activity;

  /// Chronological task trace (memory recall, web search, webpage fetch).
  final List<RunStep> steps;

  /// Pictures drawn so far in this reply.
  final List<Attachment> attachments;

  /// Web pages referenced so far in this reply.
  final List<WebSource> sources;

  /// Interactive rich cards interleaved in this reply.
  final List<MessageComponent> components;

  /// When the current activity started (for the elapsed-seconds counter).
  final DateTime? activitySince;

  const LiveText(
    this.content,
    this.reasoning, {
    this.activity = '',
    this.steps = const [],
    this.attachments = const [],
    this.sources = const [],
    this.components = const [],
    this.activitySince,
  });
  static const empty = LiveText('', '');

  LivePhase get phase => (content.isNotEmpty || components.isNotEmpty)
      ? LivePhase.answering
      : reasoning.isNotEmpty
          ? LivePhase.reasoning
          : LivePhase.waiting;

  bool get drawing => activity.contains('画') || activity.contains('改图');
  bool get working => activity.isNotEmpty && !drawing;

  /// [activityAt] is the server's start time (unix ms) when known, so the
  /// elapsed counter survives reconnects.
  LiveText copyWith({
    String? content,
    String? reasoning,
    String? activity,
    int? activityAt,
    List<RunStep>? steps,
    List<Attachment>? attachments,
    List<WebSource>? sources,
    List<MessageComponent>? components,
  }) {
    final act = activity ?? this.activity;
    DateTime? since;
    if (act.isNotEmpty) {
      since = activityAt != null && activityAt > 0
          ? DateTime.fromMillisecondsSinceEpoch(activityAt)
          : (act == this.activity ? activitySince : null) ?? DateTime.now();
    }
    return LiveText(
      content ?? this.content,
      reasoning ?? this.reasoning,
      activity: act,
      steps: steps ?? this.steps,
      attachments: attachments ?? this.attachments,
      sources: sources ?? this.sources,
      components: components ?? this.components,
      activitySince: since,
    );
  }
}

/// An image picked by the user, already compressed.
class PickedImage {
  final Uint8List bytes;
  final String path;
  final int width, height;
  const PickedImage({required this.bytes, required this.path, required this.width, required this.height});
}

@immutable
class ChatState {
  final String? conversationId;
  final String? assistantId;
  final String? modelId;

  /// Concrete model ID currently chosen by Auto mode (or last answered turn) in this conversation.
  final String? resolvedModelId;
  final bool think;
  final bool isPrivate;
  final List<Message> messages;

  /// Messages before this index are laid out upward from the scroll anchor,
  /// messages from it onward downward. Set to the latest user message when
  /// sending so the new turn starts at the top of the screen and grows down.
  final int anchor;
  final bool loading, hasMore, loadingOlder;
  final String? activeId;

  /// Bumps whenever a different conversation is shown (resets scroll).
  final int epoch;

  const ChatState({
    this.conversationId,
    this.assistantId,
    this.modelId,
    this.resolvedModelId,
    this.think = false,
    this.isPrivate = false,
    this.messages = const [],
    this.anchor = 0,
    this.loading = false,
    this.hasMore = false,
    this.loadingOlder = false,
    this.activeId,
    this.epoch = 0,
  });

  bool get isEmpty => messages.isEmpty && !loading;
  bool get busy => activeId != null;

  ChatState copyWith({
    String? conversationId,
    String? assistantId,
    Object? modelId = _keep,
    Object? resolvedModelId = _keep,
    bool? think,
    bool? isPrivate,
    List<Message>? messages,
    int? anchor,
    bool? loading,
    bool? hasMore,
    bool? loadingOlder,
    Object? activeId = _keep,
    int? epoch,
  }) =>
      ChatState(
        conversationId: conversationId ?? this.conversationId,
        assistantId: assistantId ?? this.assistantId,
        modelId: modelId == _keep ? this.modelId : modelId as String?,
        resolvedModelId: resolvedModelId == _keep ? this.resolvedModelId : resolvedModelId as String?,
        think: think ?? this.think,
        isPrivate: isPrivate ?? this.isPrivate,
        messages: messages ?? this.messages,
        anchor: anchor ?? this.anchor,
        loading: loading ?? this.loading,
        hasMore: hasMore ?? this.hasMore,
        loadingOlder: loadingOlder ?? this.loadingOlder,
        activeId: activeId == _keep ? this.activeId : activeId as String?,
        epoch: epoch ?? this.epoch,
      );
}

const _keep = Object();

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
    unawaited(ReminderBridge.instance.clearAll());
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

  void newChat({String? assistantId, bool isPrivate = false}) {
    _reset(ChatState(
      assistantId: assistantId,
      modelId: _sessionModelId,
      isPrivate: isPrivate,
      think: state.think,
      epoch: state.epoch + 1,
    ));
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

  Future<void> open(String id) async {
    if (state.conversationId == id && state.messages.isNotEmpty) return;
    final conv = ref.read(conversationsProvider).items.where((c) => c.id == id).firstOrNull;
    _reset(ChatState(
      conversationId: id,
      assistantId: conv?.assistantId,
      modelId: _sessionModelId ?? _convModeId(conv),
      resolvedModelId: conv?.modelId,
      think: state.think,
      loading: true,
      epoch: state.epoch + 1,
    ));
    final nav = _nav;
    final cached = await ref.read(storageProvider).readCache('conv_$id');
    if (nav != _nav) return;
    if (cached is List && cached.isNotEmpty) {
      final msgs = cached.map((e) => Message.fromJson((e as Map).cast())).toList();
      state = state.copyWith(messages: msgs, anchor: msgs.length, loading: false);
    }
    await _reload(id);
  }

  Future<void> _reload(String id) async {
    final api = _apiOrNull;
    if (api == null) return;
    final nav = _nav, rev = _rev;
    try {
      final d = await api.conversation(id);
      if (nav != _nav || rev != _rev || state.conversationId != id) return;
      final active = d.activeMessageId.isEmpty ? null : d.activeMessageId;
      final merged = _mergeTail(state.messages, d.messages);
      state = state.copyWith(
        messages: merged,
        anchor: active != null ? _lastUserIndex(merged) : _keepAnchor(merged),
        assistantId: d.conversation.assistantId,
        modelId: _sessionModelId ?? _convModeId(d.conversation),
        resolvedModelId: d.conversation.modelId,
        hasMore: merged.length > d.messages.length ? state.hasMore : d.hasMore,
        loading: false,
        activeId: active,
      );
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
    final original = (messages: state.messages, anchor: state.anchor);
    final op = _op = _Op();
    _rev++;

    final ts = DateTime.now().microsecondsSinceEpoch;
    final localUser = Message(
      id: 'local-u-$ts',
      conversationId: convId ?? '',
      role: 'user',
      content: text,
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
    state = state.copyWith(messages: [...base, localUser, localAsst], anchor: base.length, activeId: localAsst.id);
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
    final api = _apiOrNull;
    if (api != null && !messageId.startsWith('local-')) {
      try {
        await api.updateMessageComponentState(messageId, compId, nextState);
      } catch (_) {}
    }
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
