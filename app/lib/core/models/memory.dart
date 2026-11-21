import 'json_read.dart';

enum MemoryCategory {
  health('健康与用药'),
  family('家人'),
  profile('关于我'),
  preference('喜好习惯'),
  ongoing('近期在做'),
  episode('聊过的事');

  const MemoryCategory(this.label);
  final String label;
  static MemoryCategory parse(dynamic value) => values.where((c) => c.name == value).firstOrNull ?? profile;
}

class Memory {
  final String id, content, source;
  final MemoryCategory category;
  final String? sourceConvId;
  final bool pinned, isNew;
  final int createdAt, updatedAt;
  const Memory({required this.id, required this.category, required this.content, required this.source, this.sourceConvId,
    required this.pinned, required this.isNew, required this.createdAt, required this.updatedAt});

  factory Memory.fromJson(Map<String, dynamic> j) => Memory(
    id: asStr(j['id']), category: MemoryCategory.parse(j['category']), content: memoryToDisplay(asStr(j['content'])), source: asStr(j['source']),
    sourceConvId: asStrOrNull(j['source_conv_id']), pinned: j['pinned'] == true, isNew: j['is_new'] == true,
    createdAt: asInt(j['created_at']), updatedAt: asInt(j['updated_at']),
  );
}

/// Memories are stored in the third person ("用户…") so every model knows who
/// they are about; the family reads and edits them addressed to themselves ("你…").
/// Compounds like 用户体验 or 老年用户 are left alone.
final _subject = RegExp(
  r'(?<!老年|年轻|新|老|普通|目标|终端|企业|个人|付费|活跃|潜在|核心|海外|国内|广大|中国)用户'
  r'(?!体验|研究|画像|增长|群|数|量|端|界面|反馈|需求|调研|协议|中心|侧|运营|留存|访谈|行为|价值|隐私|数据)',
);
String memoryToDisplay(String s) => s.replaceAll(_subject, '你');

String memoryToStorage(String s) => s
    .trim()
    .replaceFirst(RegExp(r'^[我你](?!们)'), '用户')
    .replaceAll(RegExp(r'[我你]的'), '用户的')
    .replaceAllMapped(RegExp(r'([，,；;。])[我你](?!们)'), (m) => '${m[1]}用户');

/// A memory to create: typed by hand, or an import candidate. Import candidates
/// also carry retrieval keywords, an end date for ongoing items, and the existing
/// memory they would replace.
class MemoryDraft {
  final MemoryCategory category;
  final String content;
  final String keywords;
  final String? validUntil; // YYYY-MM-DD
  final String? replacesId;
  final String? replaces; // the replaced text, shown in the preview
  const MemoryDraft({required this.category, required this.content, this.keywords = '', this.validUntil, this.replacesId, this.replaces});
  factory MemoryDraft.fromJson(Map<String, dynamic> j) => MemoryDraft(
    category: MemoryCategory.parse(j['category']),
    content: memoryToDisplay(asStr(j['content'])),
    keywords: asStr(j['keywords']),
    validUntil: asStrOrNull(j['valid_until']),
    replacesId: asStrOrNull(j['replaces_id']),
    replaces: asStrOrNull(j['replaces']) == null ? null : memoryToDisplay(asStr(j['replaces'])),
  );
  Map<String, dynamic> toJson() => {
    'category': category.name,
    'content': memoryToStorage(content),
    if (keywords.isNotEmpty) 'keywords': keywords,
    if (validUntil != null) 'valid_until': validUntil,
    if (replacesId != null) 'replaces_id': replacesId,
  };

  /// Applies a user's edit: rewritten text drops the model's keywords (the server
  /// fills retrieval in later), and an end date only applies to ongoing items.
  MemoryDraft edited(MemoryDraft e) => MemoryDraft(
    category: e.category,
    content: e.content,
    keywords: e.content == content ? keywords : '',
    validUntil: e.category == MemoryCategory.ongoing ? validUntil : null,
    replacesId: replacesId,
    replaces: replaces,
  );
}

/// Organizing pasted text runs on the server; the App polls this.
class MemoryImportJob {
  final String id, status;
  final int done, total, skipped, failed;
  final List<MemoryDraft> items;
  final bool truncated;
  final String? error;
  const MemoryImportJob({required this.id, required this.status, this.done = 0, this.total = 0, this.skipped = 0, this.failed = 0,
    this.items = const [], this.truncated = false, this.error});
  bool get running => status == 'running';
  bool get failedAll => status == 'failed';
  factory MemoryImportJob.fromJson(Map<String, dynamic> j) => MemoryImportJob(
    id: asStr(j['id']), status: asStr(j['status']), done: asInt(j['done']), total: asInt(j['total']),
    skipped: asInt(j['skipped']), failed: asInt(j['failed']), truncated: j['truncated'] == true, error: asStrOrNull(j['error']),
    items: ((j['items'] as List?) ?? const []).map((e) => MemoryDraft.fromJson((e as Map).cast())).toList(),
  );
}

class MemoriesResult {
  final List<Memory> memories;
  final int total, newCount;
  final bool enabled;
  const MemoriesResult({required this.memories, required this.total, required this.newCount, required this.enabled});
  factory MemoriesResult.fromJson(Map<String, dynamic> j) => MemoriesResult(
    memories: ((j['memories'] as List?) ?? []).map((e) => Memory.fromJson((e as Map).cast())).toList(),
    total: asInt(j['total']), newCount: asInt(j['new']), enabled: j['enabled'] != false,
  );
}

/// Fixed category order, newest first within each nonempty group. Does not mutate the input.
Map<MemoryCategory, List<Memory>> groupMemories(Iterable<Memory> memories) {
  final result = <MemoryCategory, List<Memory>>{};
  for (final category in MemoryCategory.values) {
    final items = memories.where((m) => m.category == category).toList()
      ..sort((a, b) {
        final time = b.createdAt.compareTo(a.createdAt);
        return time != 0 ? time : a.id.compareTo(b.id);
      });
    if (items.isNotEmpty) result[category] = items;
  }
  return result;
}
