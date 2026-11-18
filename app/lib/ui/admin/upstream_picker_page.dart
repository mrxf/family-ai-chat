import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/admin_models.dart';
import '../../core/api.dart';
import '../../state/catalog.dart';
import '../../state/session.dart';
import '../theme.dart';
import '../widgets.dart';
import 'common.dart';
import 'model_edit_page.dart';

final _upstreamProvider = FutureProvider.autoDispose.family<List<UpstreamItem>, String>((ref, id) => ref.watch(apiProvider).adminUpstreamModels(id));

enum _Filter { all, recommended, vision, think }

/// Lists the provider's models with suggested settings; the admin ticks a few and adds them.
class UpstreamPickerPage extends ConsumerStatefulWidget {
  const UpstreamPickerPage({super.key, required this.providerId, this.firstTime = false});
  final String providerId;
  final bool firstTime;

  @override
  ConsumerState<UpstreamPickerPage> createState() => _UpstreamPickerPageState();
}

class _UpstreamPickerPageState extends ConsumerState<UpstreamPickerPage> {
  final _search = TextEditingController();
  final _selected = <String>{};
  var _filter = _Filter.all;
  var _showOthers = false;
  var _busy = false;

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  bool _match(UpstreamItem it) {
    final q = _search.text.trim().toLowerCase();
    if (q.isNotEmpty && !it.upstreamModel.toLowerCase().contains(q) && !it.displayName.toLowerCase().contains(q)) return false;
    return switch (_filter) {
      _Filter.all => true,
      _Filter.recommended => it.recommended,
      _Filter.vision => it.vision,
      _Filter.think => it.think,
    };
  }

  Future<void> _add(List<UpstreamItem> all) async {
    final picks = all.where((it) => _selected.contains(it.upstreamModel)).toList();
    if (picks.isEmpty) return;
    setState(() => _busy = true);
    try {
      final before = ref.read(catalogProvider).value?.settings.defaultModelId ?? '';
      final imageBefore = ref.read(catalogProvider).value?.settings.imageModelId ?? '';
      final added = await ref.read(apiProvider).adminBatchModels(widget.providerId, picks.map((e) => e.toModelJson()).toList());
      await ref.read(catalogProvider.notifier).reload();
      if (!mounted) return;
      HapticFeedback.mediumImpact();
      final c = ref.read(catalogProvider).value;
      final def = c?.settings.defaultModelId ?? '';
      final becameDefault = before.isEmpty && def.isNotEmpty;
      Navigator.of(context).pop();
      final img = c?.settings.imageModelId ?? '';
      final becameImage = imageBefore.isEmpty && img.isNotEmpty;
      showToast(context,
          '已添加 ${added.length} 个模型${becameDefault ? '，「${c?.model(def)?.displayName}」设为全家默认' : ''}${becameImage ? '，「${c?.model(img)?.displayName}」用来画图' : ''}');
    } on ApiError catch (e) {
      if (mounted) showToast(context, e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final data = ref.watch(_upstreamProvider(widget.providerId));
    final provider = ref.watch(catalogProvider).value?.provider(widget.providerId);
    final all = data.value ?? const <UpstreamItem>[];

    return Scaffold(
      appBar: AppBar(
        title: Column(children: [
          const Text('选择模型'),
          if (provider != null) Text(provider.name, style: TextStyle(fontSize: 12.5, color: p.muted, fontWeight: FontWeight.w400)),
        ]),
        actions: [
          if (widget.firstTime) TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('跳过')),
        ],
      ),
      bottomNavigationBar: data.hasValue && all.isNotEmpty
          ? BottomBar(
              child: Row(children: [
                Expanded(
                  child: Text(_selected.isEmpty ? '建议选 2～4 个：快的日常用，聪明的处理难题，再有一个能看图' : '已选 ${_selected.length} 个',
                      style: TextStyle(fontSize: _selected.isEmpty ? 13 : 16, color: _selected.isEmpty ? p.muted : p.text, fontWeight: FontWeight.w600, height: 1.4)),
                ),
                const SizedBox(width: 12),
                SizedBox(
                  width: 128,
                  child: PrimaryButton(label: '添加', busy: _busy, onTap: _selected.isEmpty ? null : () => _add(all)),
                ),
              ]),
            )
          : null,
      body: data.when(
        loading: () => const LoadingView(text: '正在获取模型列表…'),
        error: (e, _) => ErrorView(
          message: ApiError.from(e).message,
          onRetry: () => ref.invalidate(_upstreamProvider(widget.providerId)),
          extra: Padding(
            padding: const EdgeInsets.only(top: 8),
            child: TextButton(
              onPressed: () => Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => ModelEditPage(providerId: widget.providerId))),
              child: const Text('这个服务不提供模型列表？手动添加'),
            ),
          ),
        ),
        data: (_) => all.isEmpty
            ? ErrorView(
                message: '这个服务没有返回任何模型',
                extra: TextButton(
                  onPressed: () => Navigator.of(context).pushReplacement(MaterialPageRoute(builder: (_) => ModelEditPage(providerId: widget.providerId))),
                  child: const Text('手动添加模型'),
                ),
              )
            : _list(context, all),
      ),
    );
  }

  Widget _list(BuildContext context, List<UpstreamItem> all) {
    final p = context.pal;
    final shown = all.where(_match).toList();
    final rec = shown.where((e) => e.isChat && e.recommended).toList();
    final chat = shown.where((e) => e.isChat && !e.recommended).toList();
    final images = shown.where((e) => e.isImage).toList();
    final embeddings = shown.where((e) => e.isEmbedding).toList();
    final others = shown.where((e) => !e.isChat && !e.isImage && !e.isEmbedding).toList();
    final searching = _search.text.trim().isNotEmpty;

    final rows = <Widget>[];
    void group(String title, List<UpstreamItem> items, {String? subtitle}) {
      if (items.isEmpty) return;
      rows.add(SectionHeader('$title（${items.length}）', subtitle: subtitle));
      for (final it in items) {
        rows.add(_ItemTile(
          item: it,
          selected: _selected.contains(it.upstreamModel),
          onTap: it.added
              ? null
              : () => setState(() {
                    if (!_selected.remove(it.upstreamModel)) _selected.add(it.upstreamModel);
                  }),
        ));
      }
    }

    group('推荐', rec, subtitle: '主流的新模型，参数已经帮你调好');
    group(rec.isEmpty ? '对话模型' : '其他对话模型', chat);
    group('画图模型', images, subtitle: '加一个就够了：家人说「帮我画…」时，聊天模型会自动调用它');
    group('向量模型', embeddings, subtitle: '供记忆检索使用，添加后到「记忆检索模型」中指定');
    if (others.isNotEmpty) {
      if (_showOthers || searching) {
        group('其他模型', others, subtitle: '语音、视频等，不能用来聊天，一般不需要添加');
      } else {
        rows.add(Padding(
          padding: const EdgeInsets.only(top: 16),
          child: TextButton(onPressed: () => setState(() => _showOthers = true), child: Text('显示其他 ${others.length} 个模型（语音、视频等）')),
        ));
      }
    }
    if (shown.isEmpty) {
      rows.add(Padding(
        padding: const EdgeInsets.only(top: 60),
        child: Text('没有符合条件的模型', textAlign: TextAlign.center, style: TextStyle(color: p.muted, fontSize: 15)),
      ));
    }

    return Column(children: [
      Padding(
        padding: const EdgeInsets.fromLTRB(16, 4, 16, 0),
        child: TextField(
          controller: _search,
          onChanged: (_) => setState(() {}),
          autocorrect: false,
          decoration: InputDecoration(
            hintText: '搜索模型，例如 gpt、claude、gemini',
            prefixIcon: Icon(Icons.search_rounded, color: p.faint),
            suffixIcon: _search.text.isEmpty ? null : IconButton(icon: const Icon(Icons.close_rounded), onPressed: () => setState(_search.clear)),
            contentPadding: const EdgeInsets.symmetric(vertical: 12),
          ),
        ),
      ),
      SizedBox(
        height: 52,
        child: ListView(scrollDirection: Axis.horizontal, padding: const EdgeInsets.fromLTRB(16, 10, 16, 2), children: [
          for (final (f, label) in [(_Filter.all, '全部 ${all.length}'), (_Filter.recommended, '推荐'), (_Filter.vision, '能看图'), (_Filter.think, '能深度思考')])
            Padding(
              padding: const EdgeInsets.only(right: 8),
              child: ChoiceChip(
                label: Text(label),
                selected: _filter == f,
                onSelected: (_) => setState(() => _filter = f),
                showCheckmark: false,
                selectedColor: p.accentSoft,
                labelStyle: TextStyle(color: _filter == f ? p.accent : p.text, fontWeight: _filter == f ? FontWeight.w700 : FontWeight.w500),
                side: BorderSide(color: _filter == f ? p.accent : p.line),
                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
              ),
            ),
        ]),
      ),
      Expanded(
        child: ListView(
          keyboardDismissBehavior: ScrollViewKeyboardDismissBehavior.onDrag,
          padding: const EdgeInsets.fromLTRB(16, 0, 16, 24),
          children: rows,
        ),
      ),
    ]);
  }
}

class _ItemTile extends StatelessWidget {
  const _ItemTile({required this.item, required this.selected, this.onTap});
  final UpstreamItem item;
  final bool selected;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final it = item;
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Pressable(
        scale: 0.985,
        haptic: false,
        onTap: onTap == null
            ? null
            : () {
                HapticFeedback.selectionClick();
                onTap!();
              },
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 160),
          padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
          decoration: BoxDecoration(
            color: selected ? p.accentSoft : p.surface,
            borderRadius: BorderRadius.circular(16),
            border: Border.all(color: selected ? p.accent : p.line, width: selected ? 1.5 : 1),
          ),
          child: Row(children: [
            AnimatedSwitcher(
              duration: const Duration(milliseconds: 160),
              child: Icon(
                it.added ? Icons.check_circle_rounded : (selected ? Icons.check_circle_rounded : Icons.radio_button_unchecked_rounded),
                key: ValueKey('${it.added}$selected'),
                color: it.added ? p.faint : (selected ? p.accent : p.faint),
                size: 26,
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Opacity(
                opacity: it.added ? 0.55 : 1,
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Row(children: [
                    Flexible(child: Text(it.displayName, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600))),
                    if (it.added) ...[const SizedBox(width: 6), Tag('已添加', color: p.muted)],
                  ]),
                  if (it.displayName != it.upstreamModel) ...[
                    const SizedBox(height: 2),
                    Text(it.upstreamModel, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12.5, color: p.muted, fontFamily: 'monospace')),
                  ],
                  if (it.isChat) ...[
                    const SizedBox(height: 7),
                    Wrap(spacing: 6, runSpacing: 6, children: modelTags(context, vision: it.vision, think: it.think, inPrice: it.inputPrice, outPrice: it.outputPrice)),
                  ],
                  if (it.isEmbedding) ...[
                    const SizedBox(height: 7),
                    Tag('向量（记忆检索）', icon: Icons.manage_search_rounded, color: p.accent),
                  ],
                  if (it.isImage) ...[
                    const SizedBox(height: 7),
                    const Tag('画图', icon: Icons.palette_outlined, color: Color(0xFFDB2777)),
                  ],
                ]),
              ),
            ),
          ]),
        ),
      ),
    );
  }
}
