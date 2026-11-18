import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:family_chat/core/admin_models.dart';
import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/models.dart';
import 'package:family_chat/state/catalog.dart';
import 'package:family_chat/state/chat.dart';
import 'package:flutter_test/flutter_test.dart';

Map<String, dynamic> memoryJson({String id = 'm1', String category = 'health', int created = 1794930000000}) => {
  'id': id,
  'category': category,
  'content': '每天早上吃氨氯地平 5mg。',
  'source': 'auto',
  'source_conv_id': 'c1',
  'pinned': true,
  'is_new': true,
  'created_at': created,
  'updated_at': created + 1000,
};

class _Adapter implements HttpClientAdapter {
  _Adapter(this.respond);
  final ResponseBody Function(RequestOptions) respond;
  @override
  Future<ResponseBody> fetch(RequestOptions options, Stream<Uint8List>? requestStream, Future<void>? cancelFuture) async => respond(options);
  @override
  void close({bool force = false}) {}
}

void main() {
  test('Memory parses the shared wire contract without deriving flags or dates', () {
    final memory = Memory.fromJson(memoryJson());
    expect(memory.id, 'm1');
    expect(memory.category, MemoryCategory.health);
    expect(memory.content, '每天早上吃氨氯地平 5mg。');
    expect(memory.source, 'auto');
    expect(memory.sourceConvId, 'c1');
    expect(memory.pinned, isTrue);
    expect(memory.isNew, isTrue);
    expect(memory.createdAt, 1794930000000);
    expect(memory.updatedAt, 1794930001000);
    final manual = Memory.fromJson({...memoryJson(), 'source': 'manual', 'source_conv_id': null, 'is_new': false, 'pinned': false});
    expect(manual.sourceConvId, isNull);
    expect(manual.isNew, isFalse);
    expect(manual.pinned, isFalse);
    expect(Memory.fromJson({...memoryJson(), 'source': 'import', 'source_conv_id': ''}).source, 'import');
    expect(Memory.fromJson({...memoryJson(), 'source_conv_id': ''}).sourceConvId, isNull);
  });

  test('memory grouping uses fixed Chinese groups, hides empty groups, and sorts newest first', () {
    final items = [
      Memory.fromJson(memoryJson(id: 'p', category: 'profile', created: 50)),
      Memory.fromJson(memoryJson(id: 'old', created: 10)),
      Memory.fromJson(memoryJson(id: 'e', category: 'episode', created: 100)),
      Memory.fromJson(memoryJson(id: 'f', category: 'family', created: 2)),
      Memory.fromJson(memoryJson(id: 'new', created: 30)),
      Memory.fromJson(memoryJson(id: 'ongoing', category: 'ongoing', created: 80)),
      Memory.fromJson(memoryJson(id: 'pref', category: 'preference', created: 90)),
    ];
    final grouped = groupMemories(items);
    expect(grouped.keys.map((c) => c.label), ['健康与用药', '家人', '关于我', '喜好习惯', '近期在做', '聊过的事']);
    expect(grouped[MemoryCategory.health]!.map((m) => m.id), ['new', 'old']);
    expect(items.first.id, 'p');
    expect(groupMemories(items.where((m) => m.category == MemoryCategory.profile)).keys, [MemoryCategory.profile]);
    expect(groupMemories([]), isEmpty);
  });

  test('memory summary and drafts preserve field names and default enabled', () {
    final result = MemoriesResult.fromJson({
      'memories': [memoryJson()],
      'total': 8,
      'new': 2,
      'enabled': false,
    });
    expect(result.memories.single.category, MemoryCategory.health);
    expect(result.total, 8);
    expect(result.newCount, 2);
    expect(result.enabled, isFalse);
    expect(MemoriesResult.fromJson({}).enabled, isTrue);
    expect(MemoryDraft.fromJson({'category': 'family', 'content': ' 女儿住在上海 '}).toJson(), {'category': 'family', 'content': '女儿住在上海'});
  });

  for (final kind in ['chat', 'image', 'embedding']) {
    test('AdminModel parses $kind independently', () {
      final m = AdminModel.fromJson({'id': kind, 'kind': kind});
      expect(m.kind, kind);
      expect(m.isChat, kind == 'chat');
      expect(m.isImage, kind == 'image');
      expect(m.isEmbedding, kind == 'embedding');
    });
  }

  test('catalog excludes image and embedding from family and chat assignments', () {
    final c = Catalog(
      providers: [
        AdminProvider.fromJson({'id': 'p', 'enabled': true}),
      ],
      models: [
        for (final kind in ['chat', 'image', 'embedding']) AdminModel.fromJson({'id': kind, 'provider_id': 'p', 'kind': kind, 'enabled': true}),
      ],
      settings: ModelSettings.fromJson({'memory_model_id': 'chat', 'embedding_model_id': 'embedding'}),
    );
    expect(c.chatModels.map((m) => m.id), ['chat']);
    expect(c.usableModels.map((m) => m.id), ['chat']);
    expect(c.embeddingModels.map((m) => m.id), ['embedding']);
    expect(c.imageModels.map((m) => m.id), ['image']);
    expect(c.settings.memoryModelId, 'chat');
    expect(c.settings.embeddingModelId, 'embedding');
    expect(ModelSettings.fromJson({}).memoryModelId, '');
    expect(ModelSettings.fromJson({}).embeddingModelId, '');
  });

  test('upstream explicit kind wins over old capability flags and survives batch save', () {
    final item = UpstreamItem.fromJson({'upstream_model': 'bge-m3', 'kind': 'embedding', 'chat': true, 'image': true});
    expect(item.isEmbedding, isTrue);
    expect(item.isChat, isFalse);
    expect(item.isImage, isFalse);
    expect(item.toModelJson()['kind'], 'embedding');
    expect(UpstreamItem.fromJson({'embedding': true}).isEmbedding, isTrue);
    expect(UpstreamItem.fromJson({'chat': true}).isChat, isTrue);
    expect(UpstreamItem.fromJson({'image': true}).toModelJson()['kind'], 'image');
  });

  test('assistant use_memory survives bootstrap cache roundtrip', () {
    final assistant = Assistant.fromJson({'use_memory': false});
    expect(assistant.useMemory, isFalse);
    expect(Assistant.fromJson(assistant.toJson()).useMemory, isFalse);
    expect(Assistant.fromJson({}).useMemory, isTrue);
  });

  test('all memory API routes use the agreed methods, bodies and response envelopes', () async {
    final api = Api('http://localhost');
    final requests = <RequestOptions>[];
    api.dio.httpClientAdapter = _Adapter((request) {
      requests.add(request);
      Object body = memoryJson();
      var status = 200;
      if (request.method == 'DELETE') {
        status = 204;
        body = '';
      }
      if (request.method == 'GET') {
        body = {
          'memories': [memoryJson()],
          'total': 1,
          'new': 1,
          'enabled': true,
        };
      }
      if (request.path.endsWith('/preview') && request.method != 'DELETE') {
        status = request.method == 'POST' ? 202 : 200;
        body = {
          'id': 'j1',
          'status': request.method == 'POST' ? 'running' : 'done',
          'done': 1,
          'total': 1,
          'skipped': 2,
          'items': [
            {'category': 'health', 'content': '用户每天服药', 'keywords': '吃药', 'replaces_id': 'm0', 'replaces': '用户每天早上服药'},
          ],
        };
      }
      if (request.path.endsWith('/import')) {
        body = {
          'memories': [memoryJson()],
          'skipped': 1,
        };
      }
      if (request.method == 'POST' && request.path == '/api/memories') status = 201;
      return ResponseBody.fromString(
        status == 204 ? '' : jsonEncode(body),
        status,
        headers: {
          Headers.contentTypeHeader: ['application/json'],
        },
      );
    });
    expect((await api.memories()).total, 1);
    const draft = MemoryDraft(category: MemoryCategory.health, content: ' 每天服药 ');
    expect((await api.createMemory(draft)).id, 'm1');
    await api.updateMemory('m1', content: ' 改为晚上服药 ');
    await api.updateMemory('m1', category: MemoryCategory.family);
    await api.deleteMemory('m1');
    await api.clearMemories();
    expect((await api.startMemoryImport('从其他 AI 复制的内容')).running, isTrue);
    final job = (await api.memoryImportJob())!;
    expect(job.status, 'done');
    expect(job.skipped, 2);
    final candidate = job.items.single;
    expect(candidate.content, '你每天服药');
    expect(candidate.replaces, '你每天早上服药');
    await api.cancelMemoryImport();
    final (saved, skipped) = await api.importMemories([draft, candidate]);
    expect(saved.single.id, 'm1');
    expect(skipped, 1);
    expect(requests.map((r) => '${r.method} ${r.path}'), [
      'GET /api/memories',
      'POST /api/memories',
      'PATCH /api/memories/m1',
      'PATCH /api/memories/m1',
      'DELETE /api/memories/m1',
      'DELETE /api/memories',
      'POST /api/memories/import/preview',
      'GET /api/memories/import/preview',
      'DELETE /api/memories/import/preview',
      'POST /api/memories/import',
    ]);
    expect(requests[1].data, {'category': 'health', 'content': '每天服药'});
    expect(requests[2].data, {'content': '改为晚上服药'});
    expect(requests[3].data, {'category': 'family'});
    expect(requests[6].data, {'text': '从其他 AI 复制的内容'});
    expect(requests[9].data, {
      'items': [
        {'category': 'health', 'content': '每天服药'},
        {'category': 'health', 'content': '用户每天服药', 'keywords': '吃药', 'replaces_id': 'm0'},
      ],
    });
    api.dio.close();
  });

  test('memory errors retain the existing API error format', () async {
    final api = Api('http://localhost');
    api.dio.httpClientAdapter = _Adapter(
      (_) => ResponseBody.fromString(
        jsonEncode({
          'error': {'code': 'bad_content', 'message': '请填写 1～200 字'},
        }),
        400,
        headers: {
          Headers.contentTypeHeader: ['application/json'],
        },
      ),
    );
    await expectLater(
      api.createMemory(const MemoryDraft(category: MemoryCategory.profile, content: '')),
      throwsA(
        isA<ApiError>().having((e) => e.status, 'status', 400).having((e) => e.code, 'code', 'bad_content').having((e) => e.message, 'message', '请填写 1～200 字'),
      ),
    );
    api.dio.close();
  });

  test('memories read as "你" and are stored as "用户"', () {
    expect(memoryToDisplay('用户的父亲有高血压。'), '你的父亲有高血压。');
    expect(memoryToStorage('你的父亲有高血压。'), '用户的父亲有高血压。');
    expect(memoryToStorage('我对青霉素过敏，我的妈妈也是'), '用户对青霉素过敏，用户的妈妈也是');
    expect(memoryToStorage('  你在杭州工作 '), '用户在杭州工作');
    expect(memoryToStorage('你们全家都爱吃辣'), '你们全家都爱吃辣');
    expect(MemoryDraft(category: MemoryCategory.health, content: '你对花生过敏').toJson()['content'], '用户对花生过敏');
    // Imported work facts often mention users in another sense.
    const work = '用户是产品经理，负责用户体验和老年用户调研，用户的父亲退休了';
    expect(memoryToDisplay(work), '你是产品经理，负责用户体验和老年用户调研，你的父亲退休了');
    expect(memoryToStorage(memoryToDisplay(work)), work);
    expect(memoryToDisplay('用户名叫李芳。'), '你名叫李芳。');
  });

  test('editing an import candidate keeps what still applies', () {
    const draft = MemoryDraft(category: MemoryCategory.ongoing, content: '在装修', keywords: '装修 房子', validUntil: '2026-12-31', replacesId: 'm0', replaces: '在看房');
    final same = draft.edited(const MemoryDraft(category: MemoryCategory.ongoing, content: '在装修'));
    expect((same.keywords, same.validUntil, same.replacesId), ('装修 房子', '2026-12-31', 'm0'));
    final changed = draft.edited(const MemoryDraft(category: MemoryCategory.profile, content: '装修好了'));
    expect((changed.keywords, changed.validUntil, changed.replacesId), ('', null, 'm0'));
  });

  test('memory lookup activity stays compact while drawing activity keeps DrawingCard', () {
    const recall = LiveText('', '', activity: '正在翻看记忆…');
    expect(recall.working, isTrue);
    expect(recall.drawing, isFalse);

    const readConv = LiveText('我看看之前的对话。', '', activity: '正在查看之前的对话…');
    expect(readConv.working, isTrue);
    expect(readConv.drawing, isFalse);

    const draw = LiveText('', '', activity: '正在画图…');
    expect(draw.drawing, isTrue);
    expect(draw.working, isFalse);

    final settings = ModelSettings.fromJson({
      'embedding_model_id': 'emb-1',
      'memory_active_count': '24',
      'memory_embedded_count': '20',
      'memory_keywords_pending': '4',
    });
    expect(settings.memoryActiveCount, 24);
    expect(settings.memoryEmbeddedCount, 20);
    expect(settings.memoryKeywordsPending, 4);
  });
}
