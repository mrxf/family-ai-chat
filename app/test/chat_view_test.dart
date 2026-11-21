import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/state/chat.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/features/chat/chat_view.dart';
import 'package:family_chat/ui/features/chat/messages/messages.dart';
import 'package:family_chat/ui/theme/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

class _StubSession extends SessionNotifier {
  @override
  Session build() => Session(
        status: SessionStatus.loggedIn,
        server: 'http://localhost:8080',
        api: Api('http://localhost:8080', token: 'test'),
        boot: const Bootstrap(
          user: User(id: 'u1', username: 'zed', displayName: '小凡', avatar: '', role: 'member'),
          models: [],
          assistants: [],
          defaultModelId: '',
          voiceInput: false,
        ),
      );
}

class _FixedChat extends ChatController {
  _FixedChat(this._initial);
  final ChatState _initial;

  @override
  ChatState build() => _initial;

  @override
  Future<void> loadOlder() async {}
}

Widget _wrap(ChatState state) {
  return ProviderScope(
    overrides: [
      sessionProvider.overrideWith(_StubSession.new),
      chatProvider.overrideWith(() => _FixedChat(state)),
    ],
    child: MaterialApp(
      theme: buildTheme(Brightness.light),
      home: const Scaffold(
        body: SizedBox(width: 400, height: 700, child: ChatView()),
      ),
    ),
  );
}

void main() {
  testWidgets('short history conversation aligns user bubble to top without gap', (tester) async {
    final msgs = [
      const Message(id: 'u1', conversationId: 'c1', role: 'user', content: '给我讲个简短笑话'),
      const Message(id: 'a1', conversationId: 'c1', role: 'assistant', content: '从前有座山。'),
    ];
    await tester.pumpWidget(_wrap(ChatState(
      conversationId: 'c1',
      messages: msgs,
      anchor: msgs.length,
      hasMore: false,
      epoch: 1,
    )));
    await tester.pumpAndSettle();

    final chatTop = tester.getTopLeft(find.byType(ChatView)).dy;
    final userTop = tester.getTopLeft(find.byType(UserMessage)).dy;
    expect(userTop - chatTop, closeTo(0.0, 0.5));
  });

  testWidgets('long history conversation scrolled to top has no 56dp TopCap gap', (tester) async {
    final longReply = List.generate(30, (i) => '第 ${i + 1} 段历史故事内容，商鞅变法使得秦国逐渐强盛起来。').join('\n\n');
    final msgs = [
      const Message(id: 'u1', conversationId: 'c1', role: 'user', content: '给我讲个有意思的历史小故事'),
      Message(id: 'a1', conversationId: 'c1', role: 'assistant', content: longReply),
    ];
    await tester.pumpWidget(_wrap(ChatState(
      conversationId: 'c1',
      messages: msgs,
      anchor: msgs.length,
      hasMore: false,
      epoch: 1,
    )));
    await tester.pumpAndSettle();

    // Scroll all the way up to the start of the conversation.
    await tester.drag(find.byType(CustomScrollView), const Offset(0, 3000));
    await tester.pumpAndSettle();

    final chatTop = tester.getTopLeft(find.byType(ChatView)).dy;
    final userTop = tester.getTopLeft(find.byType(UserMessage)).dy;
    expect(userTop - chatTop, closeTo(0.0, 0.5));
  });

  testWidgets('conversation with hasMore reserves spinner space at top', (tester) async {
    final longReply = List.generate(30, (i) => '第 ${i + 1} 段历史故事内容，商鞅变法使得秦国逐渐强盛起来。').join('\n\n');
    final msgs = [
      const Message(id: 'u1', conversationId: 'c1', role: 'user', content: '更早的问题之后的一条'),
      Message(id: 'a1', conversationId: 'c1', role: 'assistant', content: longReply),
    ];
    await tester.pumpWidget(_wrap(ChatState(
      conversationId: 'c1',
      messages: msgs,
      anchor: msgs.length,
      hasMore: true,
      epoch: 1,
    )));
    await tester.pump();
    final pos = tester.state<ScrollableState>(find.byType(Scrollable)).position;
    pos.jumpTo(pos.minScrollExtent);
    await tester.pump();
    pos.jumpTo(pos.minScrollExtent);
    await tester.pump();

    final chatTop = tester.getTopLeft(find.byType(ChatView)).dy;
    final userTop = tester.getTopLeft(find.byType(UserMessage)).dy;
    expect(userTop - chatTop, closeTo(56.0, 1.0));
  });
}
