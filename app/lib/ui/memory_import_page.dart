import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/api.dart';
import '../core/models.dart';
import '../state/session.dart';
import 'memory_editor.dart';
import 'theme.dart';
import 'widgets.dart';

const memoryImportPrompt = '请把你记得的、关于我的所有信息整理出来，一条一行，按这几类分组：关于我（基本情况、职业、所在城市）、健康与用药、家人、喜好习惯、近期在做的事。只写你确定的事实，不要推测，不要写建议。';

class MemoryImportPage extends ConsumerStatefulWidget {
  const MemoryImportPage({super.key});

  @override
  ConsumerState<MemoryImportPage> createState() => _MemoryImportPageState();
}

class _MemoryImportPageState extends ConsumerState<MemoryImportPage> {
  final _text = TextEditingController();
  List<MemoryDraft>? _items;
  final _selected = <int>{};
  bool _busy = false;

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  Future<void> _preview() async {
    if (_busy) return;
    FocusScope.of(context).unfocus();
    setState(() => _busy = true);
    try {
      final items = await ref.read(apiProvider).previewMemoryImport(_text.text);
      if (!mounted) return;
      if (items.isEmpty) {
        showToast(context, '没有找到可以记住的事，换一段内容试试吧');
      } else {
        setState(() {
          _items = List.of(items);
          _selected
            ..clear()
            ..addAll(List.generate(items.length, (i) => i));
        });
      }
    } catch (e) {
      if (mounted) showToast(context, ApiError.from(e).message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
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
    setState(() => _busy = true);
    try {
      final saved = await ref.read(apiProvider).importMemories(chosen);
      if (!mounted) return;
      showToast(context, '已导入 ${saved.length} 条，小暖记住了');
      Navigator.of(context).pop(true);
    } catch (e) {
      if (mounted) showToast(context, ApiError.from(e).message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _edit(int i) async {
    final edited = await showMemoryEditor(context, title: '修改这条', initial: _items![i]);
    if (edited != null && mounted) setState(() => _items![i] = edited);
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

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final count = _text.text.runes.length;
    final reviewing = _items != null;
    return PopScope(
      canPop: !_busy,
      child: Scaffold(
        appBar: AppBar(title: const Text('从其他 AI 导入')),
        bottomNavigationBar: SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 10, 16, 12),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (_busy)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: Text(
                      reviewing ? '正在交给小暖，请稍等…' : '整理可能需要十几秒，请稍等…',
                      textAlign: TextAlign.center,
                      style: TextStyle(fontSize: 15, color: p.muted),
                    ),
                  ),
                if (reviewing && _selected.length > 200)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: Text('一次最多导入 200 条，请取消一些勾选', style: TextStyle(color: p.bad, fontSize: 15)),
                  ),
                FilledButton.icon(
                  style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(52)),
                  onPressed: _busy
                      ? null
                      : reviewing
                      ? (_selected.isEmpty || _selected.length > 200 ? null : _import)
                      : (_text.text.trim().isEmpty || count > 8000 ? null : _preview),
                  icon: _busy
                      ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                      : Icon(reviewing ? Icons.check_rounded : Icons.auto_awesome_rounded),
                  label: Text(_busy ? (reviewing ? '正在导入…' : '正在整理…') : (reviewing ? '导入 ${_selected.length} 条' : '让小暖整理')),
                ),
              ],
            ),
          ),
        ),
        body: ListView(
          key: ValueKey(reviewing),
          padding: const EdgeInsets.fromLTRB(16, 0, 16, 28),
          keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
          children: reviewing
              ? [
                  _step('3', '确认想让小暖记住的事'),
                  Text('已整理出 ${_items!.length} 条。取消不想保留的勾选，点文字可以修改。确认导入后才会记住。', style: Theme.of(context).textTheme.bodyLarge),
                  Row(
                    children: [
                      TextButton(
                        style: TextButton.styleFrom(minimumSize: const Size(44, 48)),
                        onPressed: _busy
                            ? null
                            : () => setState(() {
                                if (_selected.length == _items!.length) {
                                  _selected.clear();
                                } else {
                                  _selected.addAll(List.generate(_items!.length, (i) => i));
                                }
                              }),
                        child: Text(_selected.length == _items!.length ? '取消全选' : '全选'),
                      ),
                      const Spacer(),
                      TextButton(
                        style: TextButton.styleFrom(minimumSize: const Size(44, 48)),
                        onPressed: _busy ? null : () => setState(() => _items = null),
                        child: const Text('重新粘贴'),
                      ),
                    ],
                  ),
                  for (var i = 0; i < _items!.length; i++)
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
                                      Align(
                                        alignment: Alignment.centerLeft,
                                        child: Container(
                                          padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
                                          decoration: BoxDecoration(color: p.accentSoft, borderRadius: BorderRadius.circular(8)),
                                          child: Text(
                                            _items![i].category.label,
                                            style: TextStyle(color: p.accent, fontSize: 14, fontWeight: FontWeight.w600),
                                          ),
                                        ),
                                      ),
                                      const SizedBox(height: 10),
                                      Text(_items![i].content, style: Theme.of(context).textTheme.bodyLarge),
                                    ],
                                  ),
                                ),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                ]
              : [
                  _step('1', '请原来的 AI 整理一下'),
                  Text('复制后发给 ChatGPT / Gemini / Claude / 豆包，把它的回答整段复制回来。', style: Theme.of(context).textTheme.bodyLarge),
                  const SizedBox(height: 14),
                  MemoryPanel(
                    child: Padding(
                      padding: const EdgeInsets.all(18),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          SelectableText(memoryImportPrompt, style: TextStyle(fontSize: 16, height: 1.6, color: p.text)),
                          const SizedBox(height: 12),
                          OutlinedButton.icon(
                            style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(48)),
                            onPressed: _busy
                                ? null
                                : () async {
                                    await Clipboard.setData(const ClipboardData(text: memoryImportPrompt));
                                    if (context.mounted) showToast(context, '已复制，发给原来的 AI 就好');
                                  },
                            icon: const Icon(Icons.copy_rounded),
                            label: const Text('一键复制提示词'),
                          ),
                        ],
                      ),
                    ),
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
                      icon: const Icon(Icons.content_paste_rounded),
                      label: const Text('粘贴回答'),
                    ),
                  ),
                  TextField(
                    controller: _text,
                    enabled: !_busy,
                    minLines: 6,
                    maxLines: 14,
                    maxLength: 8000,
                    maxLengthEnforcement: MaxLengthEnforcement.none,
                    onChanged: (_) => setState(() {}),
                    style: Theme.of(context).textTheme.bodyLarge,
                    decoration: InputDecoration(
                      hintText: '长按这里粘贴，或点上面的「粘贴回答」',
                      counterText: '$count / 8000 字',
                      errorText: count > 8000 ? '内容超过 8000 字，请删短一些再整理' : null,
                    ),
                  ),
                ],
        ),
      ),
    );
  }
}
