import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'app/app.dart';
import 'core/storage.dart';
import 'state/session.dart';
import 'ui/widgets/mascot/mascot_anim.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
  final storage = await Storage.open();
  precacheMascot();
  runApp(ProviderScope(overrides: [storageProvider.overrideWithValue(storage)], child: const NuanApp()));
}
