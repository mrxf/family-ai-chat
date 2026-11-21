/// Lenient readers for server JSON: missing or mistyped fields fall back to
/// neutral values instead of throwing, so one odd field never breaks a screen.
library;

int asInt(dynamic v) => v is int ? v : (v is num ? v.toInt() : 0);
String asStr(dynamic v) => v is String ? v : '';
String? asStrOrNull(dynamic v) => v is String && v.isNotEmpty ? v : null;
