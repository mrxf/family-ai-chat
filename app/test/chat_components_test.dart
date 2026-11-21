import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/core/storage.dart';
import 'package:family_chat/state/chat.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/features/chat/components/chat_components.dart';
import 'package:family_chat/ui/features/chat/messages/messages.dart';
import 'package:family_chat/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _MemStorage implements Storage {
  _MemStorage(this.prefs);
  @override
  final SharedPreferences prefs;
  final Map<String, Object?> cache = {};
  String? token;

  @override
  Future<String?> readToken() async => token;

  @override
  Future<void> writeToken(String? t) async => token = t;

  @override
  Future<dynamic> readCache(String key) async => cache[key];

  @override
  Future<void> writeCache(String key, Object? value) async => cache[key] = value;

  @override
  Future<void> deleteCache(String key) async => cache.remove(key);

  @override
  Future<void> clearCache() async => cache.clear();
}

class _TestChatController extends ChatController {
  final List<String> sentReplies = [];

  _TestChatController(this._seed);
  final List<Message> _seed;

  @override
  ChatState build() => ChatState(conversationId: 'local-conv', messages: _seed);

  @override
  Future<void> send(
    String text, {
    List<PickedImage> images = const [],
    List<Attachment> keepAttachments = const [],
    String? editOf,
  }) async {
    sentReplies.add(text);
  }
}

Widget _wrap(
  Widget child, {
  required Storage storage,
  List<Message> seed = const [],
  _TestChatController? controller,
}) {
  return ProviderScope(
    overrides: [
      storageProvider.overrideWithValue(storage),
      chatProvider.overrideWith(() => controller ?? _TestChatController(seed)),
    ],
    child: MaterialApp(
      theme: buildTheme(Brightness.light),
      home: Scaffold(
        body: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: child,
        ),
      ),
    ),
  );
}

void main() {
  test('Message round-trips inline components and mutable state', () {
    final msg = Message.fromJson({
      'id': 'm1',
      'conversation_id': 'c1',
      'role': 'assistant',
      'content': '先看天气：\n\n记得带外套！',
      'status': 'done',
      'components': [
        {
          'id': 'comp_1',
          'kind': 'weather',
          'pos': 5,
          'data': {'location': '北京', 'condition': '晴', 'low': 1, 'high': 13},
        },
        {
          'id': 'comp_2',
          'kind': 'todo',
          'pos': 5,
          'data': {
            'title': '出行清单',
            'items': [
              {'id': 'i1', 'text': '保温杯'},
            ],
          },
          'state': {
            'done_ids': ['i1'],
          },
        },
      ],
    });
    expect(msg.components.length, 2);
    expect(msg.components[0].kind, 'weather');
    expect(msg.components[1].state['done_ids'], ['i1']);
    final reParsed = Message.fromJson(msg.toJson());
    expect(reParsed.components.length, 2);
    expect(reParsed.components[1].state['done_ids'], ['i1']);
  });

  testWidgets('InterleavedAssistantBody renders weather, charts, metrics, todo toggle, and quiz reveal',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final st = _MemStorage(await SharedPreferences.getInstance());

    const intro = '为您查好了明天北京的天气：';
    const outro = '出门早晚偏凉，记得按清单准备物品～';
    final fullContent = '$intro\n\n$outro';
    final pos = intro.runes.length;

    final seedMsg = Message(
      id: 'local-asst-1',
      conversationId: 'local-conv',
      role: 'assistant',
      content: fullContent,
      components: [
        MessageComponent(
          id: 'w1',
          kind: 'weather',
          pos: pos,
          data: const {
            'location': '北京',
            'date_label': '明天 周六',
            'condition': '晴转多云',
            'temp': 10,
            'low': 1,
            'high': 14,
            'wind': '北风 3级',
            'tip': '昼夜温差大，建议穿防风外套',
            'forecast': [
              {'day': '周六', 'condition': '晴', 'low': 1, 'high': 14},
              {'day': '周日', 'condition': '小雨', 'low': 3, 'high': 9},
            ],
          },
        ),
        MessageComponent(
          id: 't1',
          kind: 'todo',
          pos: pos,
          data: const {
            'title': '出门清单',
            'items': [
              {'id': 'i1', 'text': '保温杯', 'tag': '必带'},
              {'id': 'i2', 'text': '雨伞'},
            ],
          },
          state: const {
            'done_ids': ['i1'],
          },
        ),
        MessageComponent(
          id: 'c_pie',
          kind: 'chart',
          pos: pos,
          data: const {
            'chart_type': 'pie',
            'title': '开销占比',
            'unit': '元',
            'labels': ['交通', '餐饮'],
            'series': [
              {
                'name': '开销',
                'values': [200, 300],
              }
            ],
          },
        ),
        MessageComponent(
          id: 'q1',
          kind: 'quiz',
          pos: pos,
          data: const {
            'question': '陌生电话自称客服要求共享屏幕退费怎么办？',
            'tag': '防骗自测',
            'options': ['立刻配合共享屏幕', '直接挂断并通过官方App核实'],
            'answer_index': 1,
            'explanation': '凡是要求屏幕共享的所谓退费都是电信诈骗！',
          },
          state: const {'selected_index': -1},
        ),
        MessageComponent(
          id: 'm1',
          kind: 'metrics',
          pos: pos,
          data: const {
            'title': '今日步数与热量',
            'metrics': [
              {'label': '今日步数', 'value': '8,420', 'unit': '步', 'status': 'good'},
              {'label': '消耗热量', 'value': '310', 'unit': 'kcal', 'status': 'normal'},
            ],
          },
        ),
      ],
    );

    final ctrl = _TestChatController([seedMsg]);
    await tester.pumpWidget(
      _wrap(
        Consumer(
          builder: (_, ref, _) {
            final msg = ref.watch(chatProvider).messages.first;
            return AssistantMessage(message: msg, isLast: true);
          },
        ),
        storage: st,
        seed: [seedMsg],
        controller: ctrl,
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(WeatherCard), findsOneWidget);
    expect(find.byType(TodoCard), findsOneWidget);
    expect(find.byType(ChartCard), findsOneWidget);
    expect(find.byType(QuizCard), findsOneWidget);
    expect(find.byType(MetricsCard), findsOneWidget);
    expect(find.textContaining('明天北京的天气'), findsOneWidget);
    expect(find.text('北京'), findsOneWidget);
    expect(find.text('10°'), findsOneWidget);
    expect(find.text('1 / 2'), findsOneWidget);
    expect(find.text('开销占比'), findsOneWidget);
    expect(find.text('8,420'), findsOneWidget);
    expect(find.textContaining('记得按清单准备物品'), findsOneWidget);

    // Toggle second Todo item -> becomes "全部完成 ✓"
    await tester.ensureVisible(find.text('雨伞'));
    await tester.tap(find.text('雨伞'));
    await tester.pumpAndSettle();
    expect(find.text('全部完成 ✓'), findsOneWidget);

    // Answer the Quiz card -> reveals explanation!
    await tester.ensureVisible(find.text('直接挂断并通过官方App核实'));
    await tester.tap(find.text('直接挂断并通过官方App核实'));
    await tester.pumpAndSettle();
    expect(find.text('答对啦'), findsOneWidget);
    expect(find.textContaining('凡是要求屏幕共享的所谓退费都是电信诈骗'), findsOneWidget);
  });

  testWidgets('AskUserCard supports option selection, custom input, and auto-sends structured reply',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final st = _MemStorage(await SharedPreferences.getInstance());

    final askMsg = Message(
      id: 'local-ask-1',
      conversationId: 'local-conv',
      role: 'assistant',
      content: '先点选一下偏好，我马上为你安排：',
      components: const [
        MessageComponent(
          id: 'ask_1',
          kind: 'ask_user',
          pos: 16,
          data: {
            'title': '定制周末游',
            'questions': [
              {
                'id': 'q1',
                'question': '打算玩几天？',
                'multi_select': false,
                'options': [
                  {'label': '2天1晚', 'description': '节奏舒缓', 'recommended': true},
                  {'label': '当天往返', 'description': '不外宿'},
                ],
              },
            ],
          },
          state: {'submitted': false, 'answers': {}},
        ),
      ],
    );

    final ctrl = _TestChatController([askMsg]);
    await tester.pumpWidget(
      _wrap(
        Consumer(
          builder: (_, ref, _) {
            final msg = ref.watch(chatProvider).messages.first;
            return AssistantMessage(message: msg, isLast: true);
          },
        ),
        storage: st,
        seed: [askMsg],
        controller: ctrl,
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(AskUserCard), findsOneWidget);
    expect(find.text('推荐'), findsOneWidget);
    await tester.tap(find.text('2天1晚'));
    await tester.pumpAndSettle();

    await tester.tap(find.text('确认并发送'));
    await tester.pumpAndSettle();

    // Card transitions to locked confirmed summary and sends user reply!
    expect(find.textContaining('已确认'), findsOneWidget);
    expect(ctrl.sentReplies, ['2天1晚']);
  });

  testWidgets('AskUserCard supports skipping/closing without answering, reopening, and compact WeatherCard',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final st = _MemStorage(await SharedPreferences.getInstance());

    final msg = Message(
      id: 'local-skip-1',
      conversationId: 'local-conv',
      role: 'assistant',
      content: '顺便看一眼今天杭州天气：',
      components: const [
        MessageComponent(
          id: 'w_compact',
          kind: 'weather',
          pos: 12,
          data: {
            'location': '杭州',
            'date_label': '今天实时',
            'condition': '晴',
            'temp': 18,
            'compact': true,
          },
        ),
        MessageComponent(
          id: 'ask_skip',
          kind: 'ask_user',
          pos: 12,
          data: {
            'questions': [
              {
                'id': 'q1',
                'question': '下午想去哪走走？',
                'options': [
                  {'label': '西湖边'},
                  {'label': '龙井村'},
                ],
              },
            ],
          },
          state: {'submitted': false, 'answers': {}},
        ),
      ],
    );

    final ctrl = _TestChatController([msg]);
    await tester.pumpWidget(
      _wrap(
        Consumer(
          builder: (_, ref, _) {
            final cur = ref.watch(chatProvider).messages.first;
            return AssistantMessage(message: cur, isLast: true);
          },
        ),
        storage: st,
        seed: [msg],
        controller: ctrl,
      ),
    );
    await tester.pumpAndSettle();

    // Compact weather card renders minimal location + single temp cleanly.
    expect(find.text('杭州'), findsOneWidget);
    expect(find.text('18°'), findsOneWidget);

    // User taps "暂不选择" to dismiss the question without answering.
    await tester.tap(find.text('暂不选择'));
    await tester.pumpAndSettle();

    expect(find.text('已跳过偏好选择，直接自由对话'), findsOneWidget);
    expect(ctrl.sentReplies, isEmpty);

    // Tapping "重新展开" reopens the question card if the user changes their mind.
    await tester.tap(find.text('重新展开'));
    await tester.pumpAndSettle();
    expect(find.text('下午想去哪走走？'), findsOneWidget);
  });
}
