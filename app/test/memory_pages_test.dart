import 'dart:async';

import 'package:family_chat/core/admin_models.dart';
import 'package:family_chat/core/api.dart';
import 'package:family_chat/core/models.dart';
import 'package:family_chat/state/catalog.dart';
import 'package:family_chat/state/session.dart';
import 'package:family_chat/ui/admin/model_edit_page.dart';
import 'package:family_chat/ui/admin/models_page.dart';
import 'package:family_chat/ui/memories_page.dart';
import 'package:family_chat/ui/memory_import_page.dart';
import 'package:family_chat/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

Memory _memory(String id, String content, {MemoryCategory category = MemoryCategory.profile}) =>
    Memory(id: id, category: category, content: content, source: 'manual', pinned: true, isNew: true, createdAt: 100, updatedAt: 100);

class _FakeApi extends Api {
  _FakeApi() : super('http://localhost');
  List<Memory> items = [];
  List<MemoryDraft>? imported;
  Completer<List<MemoryDraft>>? preview;
  bool enabled = true, failCreate = false;
  int deletes = 0, creates = 0;
  Map<String, dynamic>? mePatch, modelPatch;

  @override
  Future<AdminModel> adminUpdateModel(String id, Map<String, dynamic> body) async {
    modelPatch = body;
    return AdminModel.fromJson({...body, 'id': id, 'provider_id': 'p'});
  }

  @override
  Future<MemoriesResult> memories() async =>
      MemoriesResult(memories: List.of(items), total: items.length, newCount: items.length, enabled: enabled);
  @override
  Future<User> updateMe(Map<String, dynamic> patch) async {
    mePatch = patch;
    enabled = (patch['settings'] as Map)['memory_enabled'] as bool;
    return User(
      id: 'u',
      username: 'u',
      displayName: '家人',
      avatar: '',
      role: 'member',
      settings: {'memory_enabled': enabled, 'other_setting': 'kept'},
    );
  }

  @override
  Future<Memory> createMemory(MemoryDraft item) async {
    creates++;
    if (failCreate) throw const ApiError(0, 'network', '网络连接失败，请再试一次');
    final memory = _memory('added', item.content, category: item.category);
    items.add(memory);
    return memory;
  }

  @override
  Future<void> deleteMemory(String id) async {
    deletes++;
    items.removeWhere((m) => m.id == id);
  }

  @override
  Future<List<MemoryDraft>> previewMemoryImport(String text) => preview!.future;
  @override
  Future<List<Memory>> importMemories(List<MemoryDraft> items) async {
    imported = items;
    return [for (var i = 0; i < items.length; i++) _memory('$i', items[i].content, category: items[i].category)];
  }
}

class _Session extends SessionNotifier {
  _Session(this.api);
  final Api api;
  @override
  Session build() => Session(
    status: SessionStatus.loggedIn,
    api: api,
    boot: const Bootstrap(
      user: User(id: 'u', username: 'u', displayName: '家人', avatar: '', role: 'member'),
      models: [],
      assistants: [],
      defaultModelId: '',
      voiceInput: false,
    ),
  );
  @override
  void setUser(User u) => state = Session(
    status: state.status,
    api: api,
    boot: state.boot!.copyWith(user: u),
  );
}

class _Catalog extends CatalogNotifier {
  _Catalog(this.catalog);
  final Catalog catalog;
  @override
  Future<Catalog> build() async => catalog;
  @override
  Future<void> reload() async {}
}

Widget _app(_FakeApi api, Widget page, {double textScale = 1}) => ProviderScope(
  overrides: [sessionProvider.overrideWith(() => _Session(api))],
  child: MaterialApp(
    theme: buildTheme(Brightness.light),
    builder: (ctx, child) => MediaQuery(
      data: MediaQuery.of(ctx).copyWith(textScaler: TextScaler.linear(textScale)),
      child: child!,
    ),
    home: page,
  ),
);

void main() {
  testWidgets('memory switch patches only its setting and preserves the existing list', (tester) async {
    final api = _FakeApi()..items = [_memory('m', '喜欢早上散步')];
    await tester.pumpWidget(_app(api, const MemoriesPage()));
    await tester.pumpAndSettle();
    expect(find.text('关于我 · 1'), findsOneWidget);
    expect(find.text('新'), findsOneWidget);
    await tester.tap(find.text('让小暖记住我'));
    await tester.pumpAndSettle();
    expect(api.mePatch, {
      'settings': {'memory_enabled': false},
    });
    expect(find.text('已暂停记下和使用记忆，已有的内容会保留。'), findsOneWidget);
    expect(find.text('喜欢早上散步'), findsOneWidget);
  });

  testWidgets('forgetting a memory requires confirmation and refreshes to the empty state', (tester) async {
    final api = _FakeApi()..items = [_memory('m', '喜欢早上散步')];
    await tester.pumpWidget(_app(api, const MemoriesPage()));
    await tester.pumpAndSettle();
    await tester.tap(find.text('喜欢早上散步'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('忘掉这条'));
    await tester.pumpAndSettle();
    expect(api.deletes, 0);
    await tester.tap(find.text('取消'));
    await tester.pumpAndSettle();
    expect(api.deletes, 0);
    await tester.tap(find.text('喜欢早上散步'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('忘掉这条'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('忘掉'));
    await tester.pumpAndSettle();
    expect(api.deletes, 1);
    expect(find.text('还没有记住什么。多和小暖聊聊，或者从其他 AI 导入。'), findsOneWidget);
    expect(find.text('从其他 AI 导入'), findsOneWidget);
    expect(find.text('手动添加'), findsOneWidget);
  });

  testWidgets('manual editor validates, defaults to profile, and keeps input after failure', (tester) async {
    final api = _FakeApi()..failCreate = true;
    await tester.pumpWidget(_app(api, const MemoriesPage()));
    await tester.pumpAndSettle();
    await tester.tap(find.text('手动添加'));
    await tester.pumpAndSettle();
    expect(find.text('关于我'), findsOneWidget);
    await tester.ensureVisible(find.text('保存'));
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();
    expect(find.text('先写一点想让小暖记住的事吧'), findsOneWidget);
    expect(api.creates, 0);
    await tester.enterText(find.byType(TextFormField).first, List.filled(201, '字').join());
    await tester.ensureVisible(find.text('保存'));
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();
    expect(find.text('请缩短到 200 字以内'), findsOneWidget);
    expect(api.creates, 0);
    await tester.enterText(find.byType(TextFormField).first, ' 我住在杭州 ');
    await tester.ensureVisible(find.text('保存'));
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();
    expect(find.text('网络连接失败，请再试一次'), findsOneWidget);
    expect(find.text(' 我住在杭州 '), findsOneWidget);
    api.failCreate = false;
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();
    expect(api.items.single.content, '我住在杭州');
    expect(api.items.single.category, MemoryCategory.profile);
    expect(find.text('关于我 · 1'), findsOneWidget);
  });

  testWidgets('import waits for preview, supports editing and deselection, then sends only selected items', (tester) async {
    final api = _FakeApi()..preview = Completer<List<MemoryDraft>>();
    await tester.pumpWidget(
      _app(
        api,
        Builder(
          builder: (ctx) => Scaffold(
            body: Center(
              child: TextButton(
                onPressed: () => Navigator.of(ctx).push(MaterialPageRoute(builder: (_) => const MemoryImportPage())),
                child: const Text('开始导入'),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('开始导入'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byType(TextField));
    await tester.enterText(find.byType(TextField), '我住在杭州，喜欢散步');
    await tester.pump();
    await tester.tap(find.text('让小暖整理'));
    await tester.pump();
    expect(find.text('正在整理…'), findsOneWidget);
    expect(api.imported, isNull);
    api.preview!.complete(const [
      MemoryDraft(category: MemoryCategory.profile, content: '住在杭州'),
      MemoryDraft(category: MemoryCategory.preference, content: '喜欢散步'),
    ]);
    await tester.pumpAndSettle();
    expect(find.text('导入 2 条'), findsOneWidget);
    await tester.tap(find.byType(Checkbox).last);
    await tester.pumpAndSettle();
    expect(find.text('导入 1 条'), findsOneWidget);
    await tester.tap(find.text('住在杭州'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextFormField).first, '现在住在苏州');
    await tester.ensureVisible(find.text('保存'));
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('导入 1 条'));
    await tester.pumpAndSettle();
    expect(api.imported!.single.toJson(), {'category': 'profile', 'content': '现在住在苏州'});
    expect(find.byType(MemoryImportPage), findsNothing);
  });

  testWidgets('import rejects more than 8000 characters before making a request', (tester) async {
    final api = _FakeApi();
    await tester.pumpWidget(_app(api, const MemoryImportPage()));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.byType(TextField));
    await tester.enterText(find.byType(TextField), List.filled(8001, '字').join());
    await tester.pumpAndSettle();
    expect(find.text('8001 / 8000 字'), findsOneWidget);
    final button = tester.widget<FilledButton>(find.widgetWithText(FilledButton, '让小暖整理'));
    expect(button.onPressed, isNull);
  });

  testWidgets('large text on a narrow phone keeps adding and category selection usable', (tester) async {
    tester.view.physicalSize = const Size(360, 780);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final api = _FakeApi();
    await tester.pumpWidget(_app(api, const MemoriesPage(), textScale: 1.3));
    await tester.pumpAndSettle();
    final addButton = find.widgetWithText(FilledButton, '手动添加');
    expect(tester.getSize(addButton).height, greaterThanOrEqualTo(44));
    await tester.tap(addButton);
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextFormField).first, '每天早上散步');
    final category = find.byType(DropdownButtonFormField<MemoryCategory>);
    await tester.ensureVisible(category);
    await tester.tap(category);
    await tester.pumpAndSettle();
    expect(find.text('聊过的事'), findsNothing);
    await tester.tap(find.text('喜好习惯').last);
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('保存'));
    await tester.tap(find.text('保存'));
    await tester.pumpAndSettle();
    expect(api.items.single.category, MemoryCategory.preference);
  });

  final catalog = Catalog(
    providers: [
      AdminProvider.fromJson({'id': 'p', 'name': '供应商', 'enabled': true}),
    ],
    models: [
      for (final kind in ['chat', 'image', 'embedding'])
        AdminModel.fromJson({
          'id': kind,
          'provider_id': 'p',
          'kind': kind,
          'display_name': '$kind 模型',
          'upstream_model': kind,
          'enabled': true,
          'vision': true,
          'tools': true,
          'think_effort': 'high',
          'context_window': 128000,
        }),
    ],
    settings: const ModelSettings(),
  );
  for (final kind in ['chat', 'image', 'embedding']) {
    testWidgets('admin $kind chooser shows only its model kind', (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          theme: buildTheme(Brightness.light),
          home: Builder(
            builder: (ctx) => Scaffold(
              body: TextButton(
                onPressed: () => showAdminModelChooser(ctx, catalog, title: '挑选模型', current: '', kind: kind),
                child: const Text('选择'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('选择'));
      await tester.pumpAndSettle();
      for (final other in ['chat', 'image', 'embedding']) {
        expect(find.text('$other 模型'), other == kind ? findsOneWidget : findsNothing);
      }
    });
  }

  for (final kind in ['image', 'embedding']) {
    testWidgets('$kind saves the right kind without chat capabilities and keeps valid context', (tester) async {
      final api = _FakeApi();
      await tester.pumpWidget(
        ProviderScope(
          overrides: [sessionProvider.overrideWith(() => _Session(api)), catalogProvider.overrideWith(() => _Catalog(catalog))],
          child: MaterialApp(
            theme: buildTheme(Brightness.light),
            home: ModelEditPage(modelId: kind),
          ),
        ),
      );
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).first, '修改过的模型');
      await tester.pumpAndSettle();
      await tester.tap(find.text('保存修改'));
      await tester.pumpAndSettle();
      expect(api.modelPatch!['kind'], kind);
      expect(api.modelPatch!['vision'], false);
      expect(api.modelPatch!['tools'], false);
      expect(api.modelPatch!['think_effort'], '');
      expect(api.modelPatch!['context_window'], kind == 'embedding' ? 0 : greaterThanOrEqualTo(1024));
    });
  }

  testWidgets('changing an embedding model to chat restores a valid chat context', (tester) async {
    final api = _FakeApi();
    final c = catalog.copyWith(
      models: [
        AdminModel.fromJson({
          'id': 'embedding',
          'provider_id': 'p',
          'kind': 'embedding',
          'upstream_model': 'embedding',
          'context_window': 0,
        }),
      ],
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [sessionProvider.overrideWith(() => _Session(api)), catalogProvider.overrideWith(() => _Catalog(c))],
        child: MaterialApp(
          theme: buildTheme(Brightness.light),
          home: const ModelEditPage(modelId: 'embedding'),
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.tap(find.byType(DropdownButtonFormField<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('聊天').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('保存修改'));
    await tester.pumpAndSettle();
    expect(api.modelPatch!['kind'], 'chat');
    expect(api.modelPatch!['context_window'], 128000);
  });

  testWidgets('embedding editor hides chat controls and offers vector testing', (tester) async {
    final api = _FakeApi();
    await tester.pumpWidget(
      ProviderScope(
        overrides: [sessionProvider.overrideWith(() => _Session(api)), catalogProvider.overrideWith(() => _Catalog(catalog))],
        child: MaterialApp(
          theme: buildTheme(Brightness.light),
          home: const ModelEditPage(modelId: 'embedding'),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('向量（记忆检索）'), findsOneWidget);
    expect(find.text('能看图片'), findsNothing);
    expect(find.text('会用工具'), findsNothing);
    expect(find.text('思考设置'), findsNothing);
    expect(find.text('家人看到的'), findsNothing);
    expect(find.text('测试记忆检索'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('上游模型 ID'), 250, scrollable: find.byType(Scrollable).first);
    expect(find.text('上下文长度'), findsNothing);
  });
}
