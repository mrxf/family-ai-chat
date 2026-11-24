import 'json_read.dart';

/// Supported trigger frequencies for background AI scheduled tasks.
enum ScheduledTaskKind {
  daily('每天'),
  weekdays('工作日'),
  weekly('每周'),
  monthly('每月'),
  interval('每隔'),
  once('单次');

  const ScheduledTaskKind(this.label);
  final String label;

  static ScheduledTaskKind parse(dynamic value) =>
      values.where((k) => k.name == value).firstOrNull ?? daily;
}

/// An autonomous background AI task managed on the server.
class ScheduledTask {
  final String id;
  final String title;
  final String prompt;
  final ScheduledTaskKind scheduleKind;
  final String timeOfDay;
  final String weekdays;
  final int dayOfMonth;
  final int intervalMinutes;
  final int runAt;
  final bool enabled;
  final String? modelId;
  final String targetEntityId;
  final String targetMessageId;
  final String targetComponentId;
  final String? conversationId;
  final String source;
  final int nextRunAt;
  final int lastRunAt;
  final String lastStatus;
  final String lastSummary;
  final String lastError;
  final int runCount;
  final int createdAt;
  final int updatedAt;

  const ScheduledTask({
    required this.id,
    required this.title,
    required this.prompt,
    this.scheduleKind = ScheduledTaskKind.daily,
    this.timeOfDay = '08:00',
    this.weekdays = '',
    this.dayOfMonth = 1,
    this.intervalMinutes = 0,
    this.runAt = 0,
    this.enabled = true,
    this.modelId,
    this.targetEntityId = '',
    this.targetMessageId = '',
    this.targetComponentId = '',
    this.conversationId,
    this.source = 'ai',
    this.nextRunAt = 0,
    this.lastRunAt = 0,
    this.lastStatus = '',
    this.lastSummary = '',
    this.lastError = '',
    this.runCount = 0,
    this.createdAt = 0,
    this.updatedAt = 0,
  });

  bool get hasTargetComponent =>
      targetEntityId.isNotEmpty || (targetMessageId.isNotEmpty && targetComponentId.isNotEmpty);
  bool get isRunning => lastStatus == 'running';

  String get scheduleLabel {
    switch (scheduleKind) {
      case ScheduledTaskKind.daily:
        return '每天 $timeOfDay';
      case ScheduledTaskKind.weekdays:
        return '工作日 $timeOfDay';
      case ScheduledTaskKind.weekly:
        const names = ['', '周一', '周二', '周三', '周四', '周五', '周六', '周日'];
        final labels = weekdays
            .split(',')
            .map((s) => int.tryParse(s.trim()) ?? 0)
            .where((n) => n >= 1 && n <= 7)
            .map((n) => names[n])
            .toList();
        final prefix = labels.isEmpty ? '每周' : '每${labels.join('、')}';
        return '$prefix $timeOfDay';
      case ScheduledTaskKind.monthly:
        return '每月 $dayOfMonth 日 $timeOfDay';
      case ScheduledTaskKind.interval:
        if (intervalMinutes >= 60 && intervalMinutes % 60 == 0) {
          return '每 ${intervalMinutes ~/ 60} 小时';
        }
        return '每 $intervalMinutes 分钟';
      case ScheduledTaskKind.once:
        if (runAt > 0) {
          final dt = DateTime.fromMillisecondsSinceEpoch(runAt);
          final mm = dt.month.toString().padLeft(2, '0');
          final dd = dt.day.toString().padLeft(2, '0');
          final hh = dt.hour.toString().padLeft(2, '0');
          final mi = dt.minute.toString().padLeft(2, '0');
          return '单次 $mm-$dd $hh:$mi';
        }
        return '单次执行';
    }
  }

  factory ScheduledTask.fromJson(Map<String, dynamic> j) => ScheduledTask(
        id: asStr(j['id']),
        title: asStr(j['title']),
        prompt: asStr(j['prompt']),
        scheduleKind: ScheduledTaskKind.parse(j['schedule_kind']),
        timeOfDay: asStr(j['time_of_day']),
        weekdays: asStr(j['weekdays']),
        dayOfMonth: asInt(j['day_of_month']) == 0 ? 1 : asInt(j['day_of_month']),
        intervalMinutes: asInt(j['interval_minutes']),
        runAt: asInt(j['run_at']),
        enabled: j['enabled'] != false,
        modelId: asStrOrNull(j['model_id']),
        targetEntityId: asStr(j['target_entity_id']),
        targetMessageId: asStr(j['target_message_id']),
        targetComponentId: asStr(j['target_component_id']),
        conversationId: asStrOrNull(j['conversation_id']),
        source: asStr(j['source']),
        nextRunAt: asInt(j['next_run_at']),
        lastRunAt: asInt(j['last_run_at']),
        lastStatus: asStr(j['last_status']),
        lastSummary: asStr(j['last_summary']),
        lastError: asStr(j['last_error']),
        runCount: asInt(j['run_count']),
        createdAt: asInt(j['created_at']),
        updatedAt: asInt(j['updated_at']),
      );
}

class ScheduledTaskDraft {
  final String title;
  final String prompt;
  final ScheduledTaskKind scheduleKind;
  final String timeOfDay;
  final String weekdays;
  final int dayOfMonth;
  final int intervalMinutes;
  final int runAt;
  final bool enabled;
  final String? modelId;
  final String targetEntityId;
  final String targetMessageId;
  final String targetComponentId;
  final bool runImmediately;

  const ScheduledTaskDraft({
    required this.title,
    required this.prompt,
    this.scheduleKind = ScheduledTaskKind.daily,
    this.timeOfDay = '07:00',
    this.weekdays = '',
    this.dayOfMonth = 1,
    this.intervalMinutes = 60,
    this.runAt = 0,
    this.enabled = true,
    this.modelId,
    this.targetEntityId = '',
    this.targetMessageId = '',
    this.targetComponentId = '',
    this.runImmediately = false,
  });

  Map<String, dynamic> toJson() => {
        'title': title.trim(),
        'prompt': prompt.trim(),
        'schedule_kind': scheduleKind.name,
        'time_of_day': timeOfDay,
        'weekdays': weekdays,
        'day_of_month': dayOfMonth,
        'interval_minutes': intervalMinutes,
        'run_at': runAt,
        'enabled': enabled,
        'model_id': modelId ?? '',
        'target_entity_id': targetEntityId,
        'target_message_id': targetMessageId,
        'target_component_id': targetComponentId,
        if (runImmediately) 'run_immediately': true,
      };
}

class ScheduledTaskRun {
  final String id;
  final String taskId;
  final String triggerSource;
  final String status;
  final String summary;
  final String error;
  final String messageId;
  final String conversationId;
  final int toolCalls;
  final int updatedComponents;
  final int startedAt;
  final int finishedAt;

  const ScheduledTaskRun({
    required this.id,
    required this.taskId,
    this.triggerSource = 'schedule',
    this.status = 'running',
    this.summary = '',
    this.error = '',
    this.messageId = '',
    this.conversationId = '',
    this.toolCalls = 0,
    this.updatedComponents = 0,
    this.startedAt = 0,
    this.finishedAt = 0,
  });

  factory ScheduledTaskRun.fromJson(Map<String, dynamic> j) => ScheduledTaskRun(
        id: asStr(j['id']),
        taskId: asStr(j['task_id']),
        triggerSource: asStr(j['trigger_source']),
        status: asStr(j['status']),
        summary: asStr(j['summary']),
        error: asStr(j['error']),
        messageId: asStr(j['message_id']),
        conversationId: asStr(j['conversation_id']),
        toolCalls: asInt(j['tool_calls']),
        updatedComponents: asInt(j['updated_components']),
        startedAt: asInt(j['started_at']),
        finishedAt: asInt(j['finished_at']),
      );
}

class ScheduledTasksResult {
  final List<ScheduledTask> items;
  final int activeCount;
  final int totalCount;

  const ScheduledTasksResult({
    this.items = const [],
    this.activeCount = 0,
    this.totalCount = 0,
  });

  ScheduledTask? findByTarget(String messageId, String componentId, {String? entityId}) {
    final eid = entityId ?? '';
    if (eid.isNotEmpty) {
      for (final t in items) {
        if (t.targetEntityId == eid) return t;
      }
    }
    if (messageId.isEmpty || componentId.isEmpty) return null;
    for (final t in items) {
      if (t.targetMessageId == messageId && t.targetComponentId == componentId) return t;
    }
    return null;
  }

  factory ScheduledTasksResult.fromJson(Map<String, dynamic> j) => ScheduledTasksResult(
        items: ((j['items'] as List?) ?? const []).map((e) => ScheduledTask.fromJson((e as Map).cast())).toList(),
        activeCount: asInt(j['active_count']),
        totalCount: asInt(j['total_count']),
      );
}
