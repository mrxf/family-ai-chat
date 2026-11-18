import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/models.dart';
import 'session.dart';

/// Shared by settings and the management page; never persisted on the device.
final memoriesProvider = FutureProvider.autoDispose<MemoriesResult>((ref) => ref.watch(apiProvider).memories());
