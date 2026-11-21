import 'json_read.dart';

enum ScheduleRecurrence {
  none('单次'),
  daily('每天'),
  weekdays('工作日'),
  weekly('每周'),
  monthly('每月'),
  yearly('每年');

  const ScheduleRecurrence(this.label);
  final String label;
  static ScheduleRecurrence parse(dynamic value) =>
      values.where((r) => r.name == value).firstOrNull ?? none;
}

class Schedule {
  final String id, title, note, location, recurrenceDays, status, source;
  final ScheduleRecurrence recurrence;
  final int startAt, endAt, remindBeforeMin, recurrenceUntil, completedAt, createdAt, updatedAt;
  final bool allDay;
  final List<String> completedDates;
  final String? sourceConvId;

  const Schedule({
    required this.id,
    required this.title,
    this.note = '',
    this.location = '',
    required this.startAt,
    this.endAt = 0,
    this.allDay = false,
    this.remindBeforeMin = 0,
    this.recurrence = ScheduleRecurrence.none,
    this.recurrenceDays = '',
    this.recurrenceUntil = 0,
    this.completedDates = const [],
    this.status = 'active',
    this.completedAt = 0,
    this.source = 'ai',
    this.sourceConvId,
    this.createdAt = 0,
    this.updatedAt = 0,
  });

  bool get isRecurring => recurrence != ScheduleRecurrence.none;
  bool get isCompleted => status == 'completed';

  String get recurrenceSummary {
    switch (recurrence) {
      case ScheduleRecurrence.none:
        return '';
      case ScheduleRecurrence.daily:
        return '每天';
      case ScheduleRecurrence.weekdays:
        return '每个工作日';
      case ScheduleRecurrence.weekly:
        const names = ['', '周一', '周二', '周三', '周四', '周五', '周六', '周日'];
        final labels = recurrenceDays
            .split(',')
            .map((s) => int.tryParse(s.trim()) ?? 0)
            .where((n) => n >= 1 && n <= 7)
            .map((n) => names[n])
            .toList();
        return labels.isEmpty ? '每周' : '每${labels.join('、')}';
      case ScheduleRecurrence.monthly:
        return '每月';
      case ScheduleRecurrence.yearly:
        return '每年';
    }
  }

  String get remindLabel {
    final m = remindBeforeMin;
    if (m < 0) return '不响铃';
    if (m == 0) return allDay ? '当天 09:00 提醒' : '准点提醒';
    if (m % 1440 == 0) return '提前 ${m ~/ 1440} 天提醒';
    if (m % 60 == 0) return '提前 ${m ~/ 60} 小时提醒';
    return '提前 $m 分钟提醒';
  }

  factory Schedule.fromJson(Map<String, dynamic> j) => Schedule(
        id: asStr(j['id']),
        title: asStr(j['title']),
        note: asStr(j['note']),
        location: asStr(j['location']),
        startAt: asInt(j['start_at']),
        endAt: asInt(j['end_at']),
        allDay: j['all_day'] == true,
        remindBeforeMin: asInt(j['remind_before_min']),
        recurrence: ScheduleRecurrence.parse(j['recurrence']),
        recurrenceDays: asStr(j['recurrence_days']),
        recurrenceUntil: asInt(j['recurrence_until']),
        completedDates: ((j['completed_dates'] as List?) ?? const []).map((e) => e.toString()).toList(),
        status: asStr(j['status']).isEmpty ? 'active' : asStr(j['status']),
        completedAt: asInt(j['completed_at']),
        source: asStr(j['source']),
        sourceConvId: asStrOrNull(j['source_conv_id']),
        createdAt: asInt(j['created_at']),
        updatedAt: asInt(j['updated_at']),
      );
}

class ScheduleOccurrence {
  final Schedule schedule;
  final String occurrenceDate;
  final int startAt, endAt, fireAt;
  final bool completed;

  const ScheduleOccurrence({
    required this.schedule,
    required this.occurrenceDate,
    required this.startAt,
    this.endAt = 0,
    this.fireAt = 0,
    this.completed = false,
  });

  factory ScheduleOccurrence.fromJson(Map<String, dynamic> j) => ScheduleOccurrence(
        schedule: Schedule.fromJson((j['schedule'] as Map?)?.cast<String, dynamic>() ?? const {}),
        occurrenceDate: asStr(j['occurrence_date']),
        startAt: asInt(j['start_at']),
        endAt: asInt(j['end_at']),
        fireAt: asInt(j['fire_at']),
        completed: j['completed'] == true,
      );
}

class ScheduleReminder {
  final String id, scheduleId, occurrenceDate, title, body;
  final int startAt, fireAt;
  final bool allDay;

  const ScheduleReminder({
    required this.id,
    required this.scheduleId,
    required this.occurrenceDate,
    required this.title,
    required this.body,
    required this.startAt,
    required this.fireAt,
    this.allDay = false,
  });

  factory ScheduleReminder.fromJson(Map<String, dynamic> j) => ScheduleReminder(
        id: asStr(j['id']),
        scheduleId: asStr(j['schedule_id']),
        occurrenceDate: asStr(j['occurrence_date']),
        title: asStr(j['title']),
        body: asStr(j['body']),
        startAt: asInt(j['start_at']),
        fireAt: asInt(j['fire_at']),
        allDay: j['all_day'] == true,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'schedule_id': scheduleId,
        'occurrence_date': occurrenceDate,
        'title': title,
        'body': body,
        'start_at': startAt,
        'fire_at': fireAt,
        'all_day': allDay,
      };
}

class ScheduleDraft {
  final String title, note, location, recurrenceDays;
  final int startAt, endAt, remindBeforeMin, recurrenceUntil;
  final bool allDay;
  final ScheduleRecurrence recurrence;

  const ScheduleDraft({
    required this.title,
    required this.startAt,
    this.endAt = 0,
    this.allDay = false,
    this.remindBeforeMin = 0,
    this.recurrence = ScheduleRecurrence.none,
    this.recurrenceDays = '',
    this.recurrenceUntil = 0,
    this.location = '',
    this.note = '',
  });

  Map<String, dynamic> toJson() => {
        'title': title.trim(),
        'start_at': startAt,
        'end_at': endAt,
        'all_day': allDay,
        'remind_before_min': remindBeforeMin,
        'recurrence': recurrence.name,
        'recurrence_days': recurrenceDays,
        'recurrence_until': recurrenceUntil,
        'location': location.trim(),
        'note': note.trim(),
      };
}

class SchedulesResult {
  final List<Schedule> schedules;
  final List<ScheduleOccurrence> todayOccurrences;
  final List<ScheduleOccurrence> tomorrowOccurrences;
  final List<ScheduleOccurrence> upcomingOccurrences;
  final List<ScheduleReminder> upcomingReminders;
  final int todayPending;
  final int activeTotal;

  const SchedulesResult({
    this.schedules = const [],
    this.todayOccurrences = const [],
    this.tomorrowOccurrences = const [],
    this.upcomingOccurrences = const [],
    this.upcomingReminders = const [],
    this.todayPending = 0,
    this.activeTotal = 0,
  });

  factory SchedulesResult.fromJson(Map<String, dynamic> j) => SchedulesResult(
        schedules: ((j['schedules'] as List?) ?? const []).map((e) => Schedule.fromJson((e as Map).cast())).toList(),
        todayOccurrences: ((j['today_occurrences'] as List?) ?? const [])
            .map((e) => ScheduleOccurrence.fromJson((e as Map).cast()))
            .toList(),
        tomorrowOccurrences: ((j['tomorrow_occurrences'] as List?) ?? const [])
            .map((e) => ScheduleOccurrence.fromJson((e as Map).cast()))
            .toList(),
        upcomingOccurrences: ((j['upcoming_occurrences'] as List?) ?? const [])
            .map((e) => ScheduleOccurrence.fromJson((e as Map).cast()))
            .toList(),
        upcomingReminders: ((j['upcoming_reminders'] as List?) ?? const [])
            .map((e) => ScheduleReminder.fromJson((e as Map).cast()))
            .toList(),
        todayPending: asInt(j['today_pending']),
        activeTotal: asInt(j['active_total']),
      );
}
