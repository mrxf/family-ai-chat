import 'package:flutter_test/flutter_test.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/core/share_code.dart';
import 'package:family_chat/state/conversations.dart';

void main() {
  group('XnShareCodec', () {
    test('formats and parses Douyin-style share message', () {
      final msg = XnShareCodec.formatShareMessage(
        title: '三亚亲子游攻略',
        code: 'Ab34Xy99',
        creatorName: '妈妈',
      );
      expect(msg, contains('妈妈在小暖中分享了《三亚亲子游攻略》'));
      expect(msg, contains('#xnsAb34Xy99xns#'));

      final ins = XnShareCodec.inspectText(msg);
      expect(ins.hasShare, isTrue);
      expect(ins.code, 'Ab34Xy99');
    });

    test('tolerates whitespace and zero-width characters inside token', () {
      final ins = XnShareCodec.inspectText('快来看 #xnsAb34\u200B Xy99\nxns# 哈哈');
      expect(ins.hasShare, isTrue);
      expect(ins.code, 'Ab34Xy99');
    });

    test('ShareCodeGuard tracks locally generated codes', () {
      ShareCodeGuard.clearLocalMarks();
      ShareCodeGuard.markGeneratedLocally('#xnsTestCode1xns#');
      expect(ShareCodeGuard.wasGeneratedLocally('TestCode1'), isTrue);
      expect(ShareCodeGuard.wasGeneratedLocally('OtherCode2'), isFalse);
    });
  });

  group('groupByRecency with shared conversations', () {
    test('places shared conversations in dedicated 共享 group below 置顶', () {
      final now = DateTime(2026, 11, 24, 12);
      final ts = now.millisecondsSinceEpoch;
      final groups = groupByRecency([
        Conversation(id: '1', title: '普通对话', pinned: false, createdAt: ts, updatedAt: ts),
        Conversation(id: '2', title: '家庭共享', pinned: false, isShared: true, memberCount: 3, createdAt: ts, updatedAt: ts),
        Conversation(id: '3', title: '置顶对话', pinned: true, createdAt: ts, updatedAt: ts),
      ], now);
      expect(groups.map((g) => g.label).toList(), ['置顶', '共享', '今天']);
      expect(groups[1].items.single.id, '2');
    });
  });
}
