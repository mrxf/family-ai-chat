import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/models.dart';
import '../state/chat.dart';
import '../state/memories.dart';
import '../state/session.dart';
import 'icons.dart';
import 'memory_editor.dart';
import 'memory_import_page.dart';
import 'sheets.dart';
import 'theme.dart';
import 'widgets.dart';

class MemoriesPage extends ConsumerStatefulWidget {
  const MemoriesPage({super.key});

  @override
  ConsumerState<MemoriesPage> createState() => _MemoriesPageState();
}

class _MemoriesPageState extends ConsumerState<MemoriesPage> {
  bool _busy = false;

  Future<void> _refresh() async {
    try {
      ref.invalidate(memoriesProvider);
      await ref.read(memoriesProvider.future);
    } catch (e) {
      if (mounted) showToast(context, ApiError.from(e).message);
    }
  }

  Future<void> _change(Future<void> Function() action, String message) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await action();
      if (!mounted) return;
      showToast(context, message);
      await _refresh();
    } catch (e) {
      if (mounted) showToast(context, ApiError.from(e).message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _edit([Memory? memory]) async {
    await showMemoryEditor(
      context,
      title: memory == null ? '手动添加' : '修改记住的事',
      initial: MemoryDraft(category: memory?.category ?? MemoryCategory.profile, content: memory?.content ?? ''),
      allowEpisode: memory != null,
      onSave: (draft) async {
        final api = ref.read(apiProvider);
        if (memory == null) {
          await api.createMemory(draft);
        } else {
          await api.updateMemory(memory.id, category: draft.category, content: draft.content);
        }
        if (mounted) await _refresh();
      },
    );
  }

  Future<void> _import() async {
    final imported = await Navigator.of(context).push<bool>(MaterialPageRoute(builder: (_) => const MemoryImportPage()));
    if (imported == true && mounted) await _refresh();
  }

  void _actions(Memory memory) => showActionSheet(
    context,
    title: memory.content,
    actions: [
      SheetAction(AppIcons.edit, '修改', () => _edit(memory)),
      SheetAction(AppIcons.delete, '忘掉这条', () async {
        final ok = await confirm(context, title: '忘掉这条？', body: '这条记忆会被删除，无法恢复。', ok: '忘掉', danger: true);
        if (!ok || !mounted) return;
        await _change(() => ref.read(apiProvider).deleteMemory(memory.id), '已忘掉这条');
      }, danger: true),
    ],
  );

  void _menu() => showActionSheet(
    context,
    actions: [
      SheetAction(AppIcons.inbox, '从其他 AI 导入', _import),
      SheetAction(AppIcons.clearAll, '全部清空', () async {
        final ok = await confirm(context, title: '清空所有记忆？', body: '小暖记住的所有事情都会被删除，清空后无法恢复。聊天记录会保留。', ok: '全部清空', danger: true);
        if (!ok || !mounted) return;
        await _change(() => ref.read(apiProvider).clearMemories(), '已清空所有记忆');
      }, danger: true),
    ],
  );

  @override
  Widget build(BuildContext context) {
    final data = ref.watch(memoriesProvider);
    final p = context.pal;
    final enabled = data.value?.enabled ?? (ref.watch(sessionProvider).user?.settings['memory_enabled'] != false);
    final groups = groupMemories(data.value?.memories ?? const []);
    return Scaffold(
      appBar: AppBar(
        title: const Text('小暖记住的事'),
        actions: [IconButton(tooltip: '更多', onPressed: _busy ? null : _menu, icon: const Icon(AppIcons.more))],
      ),
      bottomNavigationBar: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 12),
          child: FilledButton.icon(
            style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(52)),
            onPressed: _busy ? null : () => _edit(),
            icon: const Icon(AppIcons.plus, size: 20),
            label: const Text('手动添加'),
          ),
        ),
      ),
      body: RefreshIndicator(
        onRefresh: _refresh,
        child: ListView(
          physics: const AlwaysScrollableScrollPhysics(),
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
          children: [
            MemoryPanel(
              child: Padding(
                padding: const EdgeInsets.all(18),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text('小暖会从聊天里记下关于你和家人的事，换哪个模型都记得。只有你自己能看到。', style: Theme.of(context).textTheme.bodyLarge),
                    const SizedBox(height: 12),
                    SwitchListTile.adaptive(
                      contentPadding: EdgeInsets.zero,
                      title: const Text('让小暖记住我', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
                      value: enabled,
                      onChanged: _busy || !data.hasValue
                          ? null
                          : (v) => _change(() async {
                              final session = ref.read(sessionProvider.notifier);
                              final user = await ref.read(apiProvider).updateMe({
                                'settings': {'memory_enabled': v},
                              });
                              session.setUser(user);
                            }, v ? '小暖会继续记住你' : '已关闭，已有的记忆仍可查看和管理'),
                    ),
                    if (!enabled) Text('已暂停记下和使用记忆，已有的内容会保留。', style: TextStyle(color: p.muted, fontSize: 15, height: 1.5)),
                    if (data.value?.memories.isNotEmpty ?? false)
                      Align(
                        alignment: Alignment.centerLeft,
                        child: TextButton.icon(
                          style: TextButton.styleFrom(minimumSize: const Size(44, 48), padding: const EdgeInsets.symmetric(horizontal: 4)),
                          onPressed: _busy ? null : _import,
                          icon: const Icon(AppIcons.inbox, size: 19),
                          label: const Text('从 ChatGPT、Gemini 等导入'),
                        ),
                      ),
                    if (_busy) const LinearProgressIndicator(),
                  ],
                ),
              ),
            ),
            ...data.when(
              skipLoadingOnRefresh: false,
              loading: () => [
                const Padding(
                  padding: EdgeInsets.all(40),
                  child: Center(child: CircularProgressIndicator()),
                ),
              ],
              error: (e, _) => [
                Padding(
                  padding: const EdgeInsets.all(24),
                  child: Column(
                    children: [
                      Text(ApiError.from(e).message, style: Theme.of(context).textTheme.bodyLarge, textAlign: TextAlign.center),
                      const SizedBox(height: 12),
                      OutlinedButton(onPressed: _refresh, child: const Text('再试一次')),
                    ],
                  ),
                ),
              ],
              data: (result) => result.memories.isEmpty
                  ? [
                      Padding(
                        padding: const EdgeInsets.fromLTRB(16, 32, 16, 12),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            const Center(child: Mascot(size: 100)),
                            const SizedBox(height: 20),
                            Text('还没有记住什么。多和小暖聊聊，或者从其他 AI 导入。', textAlign: TextAlign.center, style: Theme.of(context).textTheme.bodyLarge),
                            const SizedBox(height: 20),
                            OutlinedButton.icon(
                              style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(50)),
                              onPressed: _busy ? null : _import,
                              icon: const Icon(AppIcons.inbox, size: 19),
                              label: const Text('从其他 AI 导入'),
                            ),
                          ],
                        ),
                      ),
                    ]
                  : [
                      for (final group in groups.entries) ...[
                        Padding(
                          padding: const EdgeInsets.fromLTRB(8, 24, 8, 10),
                          child: Text(
                            '${group.key.label} · ${group.value.length}',
                            style: TextStyle(fontSize: 17, fontWeight: FontWeight.w600, color: p.muted),
                          ),
                        ),
                        MemoryPanel(
                          child: Column(
                            children: [
                              for (final (i, memory) in group.value.indexed) ...[
                                if (i > 0) Divider(height: 1, indent: 18, endIndent: 18, color: p.line),
                                Pressable(
                                  scale: 0.99,
                                  onTap: _busy ? null : () => _actions(memory),
                                  child: Padding(
                                    padding: const EdgeInsets.fromLTRB(18, 16, 18, 12),
                                    child: Column(
                                      crossAxisAlignment: CrossAxisAlignment.stretch,
                                      children: [
                                        if (memory.isNew)
                                          Align(
                                            alignment: Alignment.centerLeft,
                                            child: Container(
                                              margin: const EdgeInsets.only(bottom: 8),
                                              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                                              decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(8)),
                                              child: Text(
                                                '新',
                                                style: TextStyle(color: p.accent, fontSize: 13, fontWeight: FontWeight.w700),
                                              ),
                                            ),
                                          ),
                                        Text(memory.content, style: Theme.of(context).textTheme.bodyLarge),
                                        if (memory.sourceConvId != null)
                                          Align(
                                            alignment: Alignment.centerLeft,
                                            child: TextButton.icon(
                                              style: TextButton.styleFrom(minimumSize: const Size(44, 44), foregroundColor: p.muted, padding: EdgeInsets.zero),
                                              onPressed: _busy
                                                  ? null
                                                  : () {
                                                      ref.read(chatProvider.notifier).open(memory.sourceConvId!);
                                                      Navigator.of(context).popUntil((r) => r.isFirst);
                                                    },
                                              icon: const Icon(AppIcons.chatBubble, size: 15),
                                              label: const Text('来自对话'),
                                            ),
                                          ),
                                      ],
                                    ),
                                  ),
                                ),
                              ],
                            ],
                          ),
                        ),
                      ],
                    ],
            ),
          ],
        ),
      ),
    );
  }
}
