import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../../../state/session.dart';
import '../../theme/icons.dart';
import 'memory_editor.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';

const memoryImportPrompt = '''我想把你对我的了解搬到另一个 AI 助手上。请把你记得的关于我的所有信息完整列出来，包括你保存的记忆、我设置过的个性化要求，以及从我们以前的对话里了解到的我的长期情况。

要求：
1. 按下面几类分组，一行写一件事：
【关于我】称呼、年龄、职业或退休情况、所在城市、生活习惯
【健康与用药】我和家人的疾病、过敏、正在吃的药和剂量、手术和体检的重要情况（换过药、停过药的，写最新情况并注明时间）
【家人】家里有谁、怎么称呼、年龄或生日、在哪里工作上学
【喜好习惯】口味和忌口、兴趣爱好，以及我希望你怎么回答我（语气、详细还是简短、格式）
【近期在做】还在进行的事和计划，知道时间的写上年月
【其他】不属于上面几类、但你觉得重要的事
2. 用简体中文写，原来是英文的也翻译过来，药名写中文名。
3. 只写我明确说过的事实，不要推测，不要给建议；已经结束的旧安排不用写。
4. 尽量完整，不要因为篇幅省略。直接输出列表，不用开头和结尾的客套话。''';

const _maxRunes = 30000;
const _maxItems = 300;

class MemoryImportPage extends ConsumerStatefulWidget {
  const MemoryImportPage({super.key});

  @override
  ConsumerState<MemoryImportPage> createState() => _MemoryImportPageState();
}

class _MemoryImportPageState extends ConsumerState<MemoryImportPage> {
  final _text = TextEditingController();
  MemoryImportJob? _job; // running, or finished with items to review
  List<MemoryDraft>? _items;
  final _selected = <int>{};
  Timer? _poll;
  bool _busy = false; // starting a job or saving
  bool _resuming = true;
  bool _promptOpen = false;

  @override
  void initState() {
    super.initState();
    _resume();
  }

  @override
  void dispose() {
    _poll?.cancel();
    _text.dispose();
    super.dispose();
  }

  Api get _api => ref.read(apiProvider);

  /// Organizing keeps going on the server when the page is closed; pick it back up.
  Future<void> _resume() async {
    try {
      final job = await _api.memoryImportJob();
      if (!mounted) return;
      if (job != null && job.running) {
        setState(() => _job = job);
        _startPolling();
      } else if (job != null && job.status == 'done') {
        _show(job);
      }
    } catch (_) {
      // Nothing to resume; start fresh.
    } finally {
      if (mounted) setState(() => _resuming = false);
    }
  }

  void _startPolling() {
    _poll?.cancel();
    _poll = Timer.periodic(const Duration(milliseconds: 1500), (_) => _check());
  }

  Future<void> _check() async {
    MemoryImportJob? job;
    try {
      job = await _api.memoryImportJob();
    } catch (_) {
      return; // a flaky network: try again on the next tick
    }
    if (!mounted || _job == null) return;
    if (job == null || job.id != _job!.id) {
      _poll?.cancel();
      setState(() => _job = null);
      showToast(context, '整理被中断了，请再试一次');
      return;
    }
    if (job.running) {
      setState(() => _job = job);
      return;
    }
    _poll?.cancel();
    if (job.failedAll) {
      setState(() => _job = null);
      showToast(context, job.error ?? '暂时无法整理，请稍后再试');
      return;
    }
    _show(job);
  }

  void _show(MemoryImportJob job) {
    if (job.items.isEmpty) {
      setState(() => _job = null);
      _api.cancelMemoryImport().ignore();
      showToast(context, job.skipped > 0 ? '这些事小暖都已经记着了，不用再导入' : '没有找到可以记住的事，换一段内容试试吧');
      return;
    }
    setState(() {
      _job = job;
      _items = List.of(job.items);
      _selected
        ..clear()
        ..addAll(List.generate(job.items.length, (i) => i));
    });
  }

  Future<void> _start() async {
    if (_busy) return;
    FocusScope.of(context).unfocus();
    setState(() => _busy = true);
    try {
      final job = await _api.startMemoryImport(_text.text);
      if (!mounted) return;
      setState(() => _job = job);
      _startPolling();
    } catch (e) {
      if (mounted) showToast(context, ApiError.from(e).message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Stops organizing, or discards a finished preview, and goes back to pasting.
  void _discard() {
    _poll?.cancel();
    _api.cancelMemoryImport().ignore();
    setState(() {
      _job = null;
      _items = null;
      _selected.clear();
    });
  }

  Future<void> _import() async {
    if (_busy) return;
    final chosen = [
      for (var i = 0; i < _items!.length; i++)
        if (_selected.contains(i)) _items![i],
    ];
    if (chosen.any((item) => item.content.trim().isEmpty || item.content.trim().runes.length > 200)) {
      showToast(context, '每条请保留 1～200 字，点开过长或空白的条目修改一下');
      return;
    }
    final memoryOn = ref.read(sessionProvider).user?.settings['memory_enabled'] != false;
    var turnOn = false;
    if (!memoryOn) {
      final choice = await _askTurnOn();
      if (choice == null || !mounted) return;
      turnOn = choice;
    }
    setState(() => _busy = true);
    try {
      final (saved, skipped) = await _api.importMemories(chosen);
      if (turnOn) {
        final user = await _api.updateMe({
          'settings': {'memory_enabled': true},
        });
        ref.read(sessionProvider.notifier).setUser(user);
      }
      if (!mounted) return;
      final extra = skipped > 0 ? '，另有 $skipped 条已经记着了' : '';
      showToast(context, memoryOn || turnOn ? '已导入 ${saved.length} 条$extra，小暖记住了' : '已导入 ${saved.length} 条$extra。打开「让小暖记住我」后聊天时才会用上');
      Navigator.of(context).pop(true);
    } catch (e) {
      if (mounted) showToast(context, ApiError.from(e).message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// true = import and turn memory on, false = only import, null = cancelled.
  Future<bool?> _askTurnOn() => showDialog<bool>(
    context: context,
    builder: (ctx) => AlertDialog(
      title: const Text('记忆功能现在是关着的'),
      content: const Text('导入后，要打开「让小暖记住我」，小暖聊天时才会用上这些事。要顺便打开吗？', style: TextStyle(fontSize: 16, height: 1.5)),
      actions: [
        TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('只导入')),
        FilledButton(
          style: FilledButton.styleFrom(minimumSize: const Size(88, 46)),
          onPressed: () => Navigator.pop(ctx, true),
          child: const Text('导入并打开'),
        ),
      ],
    ),
  );

  Future<void> _edit(int i) async {
    final edited = await showMemoryEditor(context, title: '修改这条', initial: _items![i]);
    if (edited != null && mounted) setState(() => _items![i] = _items![i].edited(edited));
  }

  Widget _step(String number, String title) => Padding(
    padding: const EdgeInsets.only(top: 22, bottom: 12),
    child: Row(
      children: [
        Container(
          width: 32,
          height: 32,
          alignment: Alignment.center,
          decoration: BoxDecoration(color: context.pal.accentSoft, shape: BoxShape.circle),
          child: Text(
            number,
            style: TextStyle(color: context.pal.accent, fontSize: 17, fontWeight: FontWeight.w700),
          ),
        ),
        const SizedBox(width: 10),
        Expanded(
          child: Text(title, style: const TextStyle(fontSize: 19, fontWeight: FontWeight.w700)),
        ),
      ],
    ),
  );

  Widget _tag(String text, {bool strong = false}) {
    final p = context.pal;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
      decoration: BoxDecoration(color: strong ? p.accent : p.accentSoft, borderRadius: BorderRadius.circular(8)),
      child: Text(
        text,
        style: TextStyle(color: strong ? Colors.white : p.accent, fontSize: 14, fontWeight: FontWeight.w600),
      ),
    );
  }

  Widget _notice(String text) {
    final p = context.pal;
    return Padding(
      padding: const EdgeInsets.only(top: 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.only(top: 2),
            child: AppIcon(AppIcons.info, size: 20, color: p.bad),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(text, style: TextStyle(fontSize: 15, height: 1.5, color: p.bad)),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final reviewing = _items != null;
    final organizing = !reviewing && _job != null;
    return PopScope(
      canPop: !_busy,
      child: Scaffold(
        appBar: AppBar(title: const Text('从其他 AI 导入')),
        bottomNavigationBar: _resuming ? null : _bottom(reviewing, organizing),
        body: _resuming
            ? const Center(child: CircularProgressIndicator())
            : organizing
            ? _progress()
            : ListView(
                key: ValueKey(reviewing),
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 28),
                keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
                children: reviewing ? _review() : _input(),
              ),
      ),
    );
  }

  Widget _bottom(bool reviewing, bool organizing) {
    final p = context.pal;
    final count = _text.text.runes.length;
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 10, 16, 12),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (reviewing && _selected.length > _maxItems)
              Padding(
                padding: const EdgeInsets.only(bottom: 10),
                child: Text('一次最多导入 $_maxItems 条，请取消一些勾选', style: TextStyle(color: p.bad, fontSize: 15)),
              ),
            if (organizing)
              OutlinedButton.icon(
                style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(52)),
                onPressed: _discard,
                icon: const AppIcon(AppIcons.close, size: 19),
                label: const Text('取消整理'),
              )
            else
              FilledButton.icon(
                style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(52)),
                onPressed: _busy
                    ? null
                    : reviewing
                    ? (_selected.isEmpty || _selected.length > _maxItems ? null : _import)
                    : (_text.text.trim().isEmpty || count > _maxRunes ? null : _start),
                icon: _busy
                    ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                    : AppIcon(reviewing ? AppIcons.check : AppIcons.sparkles, size: 20),
                label: Text(_busy ? (reviewing ? '正在导入…' : '正在提交…') : (reviewing ? '导入 ${_selected.length} 条' : '让小暖整理')),
              ),
          ],
        ),
      ),
    );
  }

  Widget _progress() {
    final p = context.pal;
    final job = _job!;
    final merging = job.total > 1 && job.done >= job.total;
    final label = merging ? '快好了，正在去掉重复的…' : (job.total > 1 ? '正在整理，已完成 ${job.done} / ${job.total} 段' : '小暖正在整理…');
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(28),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Mascot(size: 96),
            const SizedBox(height: 24),
            Text(
              label,
              style: const TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 16),
            ClipRRect(
              borderRadius: BorderRadius.circular(6),
              child: LinearProgressIndicator(minHeight: 8, value: job.total > 1 && !merging ? job.done / job.total : null),
            ),
            const SizedBox(height: 16),
            Text(
              '内容多的话要一两分钟。可以先去做别的事，回到这里就能看到结果。',
              textAlign: TextAlign.center,
              style: TextStyle(fontSize: 16, height: 1.6, color: p.muted),
            ),
          ],
        ),
      ),
    );
  }

  List<Widget> _input() {
    final p = context.pal;
    final count = _text.text.runes.length;
    return [
      _step('1', '请原来的 AI 整理一下'),
      Text('复制下面这段话，发给 ChatGPT / Gemini / Claude / 豆包，再把它的回答整段复制回来。', style: Theme.of(context).textTheme.bodyLarge),
      const SizedBox(height: 14),
      MemoryPanel(
        child: Padding(
          padding: const EdgeInsets.all(18),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              AnimatedSize(
                duration: const Duration(milliseconds: 200),
                alignment: Alignment.topCenter,
                child: _promptOpen
                    ? SelectableText(memoryImportPrompt, style: TextStyle(fontSize: 16, height: 1.6, color: p.text))
                    : Text(
                        memoryImportPrompt,
                        maxLines: 3,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 16, height: 1.6, color: p.text),
                      ),
              ),
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton(
                  style: TextButton.styleFrom(minimumSize: const Size(44, 44), padding: EdgeInsets.zero),
                  onPressed: () => setState(() => _promptOpen = !_promptOpen),
                  child: Text(_promptOpen ? '收起' : '展开全文'),
                ),
              ),
              OutlinedButton.icon(
                style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(48)),
                onPressed: _busy
                    ? null
                    : () async {
                        await Clipboard.setData(const ClipboardData(text: memoryImportPrompt));
                        if (mounted) showToast(context, '已复制，发给原来的 AI 就好');
                      },
                icon: const AppIcon(AppIcons.copy, size: 19),
                label: const Text('一键复制这段话'),
              ),
            ],
          ),
        ),
      ),
      Padding(
        padding: const EdgeInsets.only(top: 10),
        child: Text('也可以直接复制 ChatGPT「设置 → 个性化 → 记忆」或 Gemini「保存的信息」里的内容。', style: TextStyle(fontSize: 15, height: 1.5, color: p.muted)),
      ),
      _step('2', '把它的回答粘贴到这里'),
      Align(
        alignment: Alignment.centerRight,
        child: TextButton.icon(
          style: TextButton.styleFrom(minimumSize: const Size(44, 48)),
          onPressed: _busy
              ? null
              : () async {
                  final data = await Clipboard.getData(Clipboard.kTextPlain);
                  if (!mounted) return;
                  setState(() => _text.text = data?.text ?? '');
                },
          icon: const AppIcon(AppIcons.paste, size: 19),
          label: const Text('粘贴回答'),
        ),
      ),
      TextField(
        controller: _text,
        enabled: !_busy,
        minLines: 6,
        maxLines: 14,
        maxLength: _maxRunes,
        maxLengthEnforcement: MaxLengthEnforcement.none,
        onChanged: (_) => setState(() {}),
        style: Theme.of(context).textTheme.bodyLarge,
        decoration: InputDecoration(
          hintText: '长按这里粘贴，或点上面的「粘贴回答」',
          counterText: '$count / $_maxRunes 字',
          errorText: count > _maxRunes ? '内容超过 $_maxRunes 字，请分两次导入' : null,
        ),
      ),
    ];
  }

  List<Widget> _review() {
    final p = context.pal;
    final job = _job!;
    final items = _items!;
    final updates = items.where((i) => i.replacesId != null).length;
    final summary = StringBuffer('整理出 ${items.length} 条');
    if (updates > 0) summary.write('，其中 $updates 条会更新原来记的');
    summary.write('。');
    if (job.skipped > 0) summary.write('另有 ${job.skipped} 条小暖已经记着了，不再重复。');
    summary.write('取消不想保留的勾选，点文字可以修改。确认导入后才会记住。');
    final order = List.generate(items.length, (i) => i)..sort((a, b) => items[a].category.index.compareTo(items[b].category.index));
    final widgets = <Widget>[
      _step('3', '确认想让小暖记住的事'),
      Text(summary.toString(), style: Theme.of(context).textTheme.bodyLarge),
      if (job.failed > 0) _notice('有 ${job.failed} 段内容没整理好，可以稍后把那部分单独粘贴再导入一次。'),
      if (job.truncated) _notice('内容太多，只列出了前 $_maxItems 条。'),
      Row(
        children: [
          TextButton(
            style: TextButton.styleFrom(minimumSize: const Size(44, 48)),
            onPressed: _busy
                ? null
                : () => setState(() {
                    if (_selected.length == items.length) {
                      _selected.clear();
                    } else {
                      _selected.addAll(List.generate(items.length, (i) => i));
                    }
                  }),
            child: Text(_selected.length == items.length ? '取消全选' : '全选'),
          ),
          const Spacer(),
          TextButton(
            style: TextButton.styleFrom(minimumSize: const Size(44, 48)),
            onPressed: _busy ? null : _discard,
            child: const Text('重新粘贴'),
          ),
        ],
      ),
    ];
    MemoryCategory? group;
    for (final i in order) {
      final item = items[i];
      if (item.category != group) {
        group = item.category;
        widgets.add(
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 14, 8, 10),
            child: Text(
              '${group.label} · ${items.where((x) => x.category == group).length}',
              style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700, color: p.muted),
            ),
          ),
        );
      }
      widgets.add(
        Padding(
          padding: const EdgeInsets.only(bottom: 10),
          child: MemoryPanel(
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.only(top: 7),
                  child: Checkbox(
                    value: _selected.contains(i),
                    onChanged: _busy
                        ? null
                        : (v) => setState(() {
                            if (v == true) {
                              _selected.add(i);
                            } else {
                              _selected.remove(i);
                            }
                          }),
                  ),
                ),
                Expanded(
                  child: Pressable(
                    scale: 0.99,
                    onTap: _busy ? null : () => _edit(i),
                    child: Padding(
                      padding: const EdgeInsets.fromLTRB(0, 16, 16, 16),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          if (item.replacesId != null) ...[Align(alignment: Alignment.centerLeft, child: _tag('更新', strong: true)), const SizedBox(height: 10)],
                          Text(item.content, style: Theme.of(context).textTheme.bodyLarge),
                          if (item.replaces != null) ...[
                            const SizedBox(height: 8),
                            Text('原来记的：${item.replaces}', style: TextStyle(fontSize: 15, height: 1.5, color: p.muted)),
                          ],
                        ],
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      );
    }
    return widgets;
  }
}
