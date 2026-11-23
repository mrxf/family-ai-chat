import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../core/api.dart';
import '../../../core/models/models.dart';
import '../../adaptive/adaptive_modal.dart';
import '../../theme/theme.dart';
import '../../widgets/base_widgets.dart';

Future<MemoryDraft?> showMemoryEditor(
  BuildContext context, {
  String title = '手动添加',
  MemoryDraft initial = const MemoryDraft(category: MemoryCategory.profile, content: ''),
  bool allowEpisode = false,
  Future<void> Function(MemoryDraft)? onSave,
}) => showAdaptiveModal<MemoryDraft>(
  context: context,
  maxWidth: 500,
  isDismissible: false,
  enableDrag: false,
  builder: (_) => _MemoryEditor(title: title, initial: initial, allowEpisode: allowEpisode, onSave: onSave),
);

class _MemoryEditor extends StatefulWidget {
  const _MemoryEditor({required this.title, required this.initial, required this.allowEpisode, this.onSave});
  final String title;
  final MemoryDraft initial;
  final bool allowEpisode;
  final Future<void> Function(MemoryDraft)? onSave;

  @override
  State<_MemoryEditor> createState() => _MemoryEditorState();
}

class _MemoryEditorState extends State<_MemoryEditor> {
  late final _content = TextEditingController(text: widget.initial.content);
  late MemoryCategory _category = widget.initial.category;
  final _form = GlobalKey<FormState>();
  bool _saving = false;

  @override
  void dispose() {
    _content.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (_saving) return;
    if (!_form.currentState!.validate()) return;
    final draft = MemoryDraft(category: _category, content: _content.text.trim());
    setState(() => _saving = true);
    try {
      await widget.onSave?.call(draft);
      if (mounted) Navigator.pop(context, draft);
    } catch (e) {
      if (mounted) showToast(context, ApiError.from(e).message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) => PopScope(
    canPop: !_saving,
    child: SafeArea(
      top: false,
      child: SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(20, 4, 20, MediaQuery.viewInsetsOf(context).bottom + 20),
        child: Form(
          key: _form,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(widget.title, style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 18),
              TextFormField(
                controller: _content,
                enabled: !_saving,
                minLines: 3,
                maxLines: 7,
                maxLength: 200,
                maxLengthEnforcement: MaxLengthEnforcement.none,
                style: Theme.of(context).textTheme.bodyLarge,
                decoration: const InputDecoration(labelText: '想让小暖记住什么？', hintText: '例如：我每天早上会去公园散步'),
                validator: (v) {
                  final n = (v ?? '').trim().runes.length;
                  return n == 0 ? '先写一点想让小暖记住的事吧' : (n > 200 ? '请缩短到 200 字以内' : null);
                },
              ),
              const SizedBox(height: 12),
              DropdownButtonFormField<MemoryCategory>(
                initialValue: _category,
                isExpanded: true,
                decoration: const InputDecoration(labelText: '放在哪一类'),
                items: [
                  for (final c in MemoryCategory.values)
                    if (widget.allowEpisode || c != MemoryCategory.episode)
                      DropdownMenuItem(
                        value: c,
                        child: Text(c.label, style: const TextStyle(fontSize: 17)),
                      ),
                ],
                onChanged: _saving ? null : (v) => setState(() => _category = v!),
              ),
              const SizedBox(height: 20),
              FilledButton(
                style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(50)),
                onPressed: _saving ? null : _save,
                child: _saving ? const SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2)) : const Text('保存'),
              ),
              TextButton(
                style: TextButton.styleFrom(minimumSize: const Size.fromHeight(48), foregroundColor: context.pal.muted),
                onPressed: _saving ? null : () => Navigator.pop(context),
                child: const Text('取消'),
              ),
            ],
          ),
        ),
      ),
    ),
  );
}

/// The same rounded, bordered surface as the settings groups.
class MemoryPanel extends StatelessWidget {
  const MemoryPanel({super.key, required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) => Material(
    clipBehavior: Clip.antiAlias,
    color: context.pal.surface,
    shape: RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(20),
      side: BorderSide(color: context.pal.line),
    ),
    child: child,
  );
}
