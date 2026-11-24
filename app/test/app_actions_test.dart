import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:family_chat/core/models/models.dart';
import 'package:family_chat/state/app_actions.dart';

MessageComponent action({
  String id = 'a1',
  String device = 'dev_this',
  String kind = 'set_settings',
  Map<String, dynamic> patch = const {'font_scale': 1.15},
  Map<String, dynamic> state = const {'status': 'pending'},
}) => MessageComponent(id: id, kind: 'app_action', data: {'target_device_id': device, 'action': kind, 'patch': patch}, state: state);

void main() {
  late Map<String, dynamic> values;
  late List<Map<String, dynamic>> receipts;
  late List<Map<String, dynamic>> acknowledgements;
  late int applications;
  late bool offline;
  AppActionExecutor executor() => AppActionExecutor(
    deviceId: 'dev_this',
    readValues: () => {...values},
    applyValues: (patch) async {
      applications++;
      values.addAll(patch);
    },
    readReceipts: () => (jsonDecode(jsonEncode(receipts)) as List).map((r) => (r as Map).cast<String, dynamic>()).toList(),
    saveReceipts: (next) async {
      receipts = next;
    },
    writeState: (_, _, state) async {
      if (offline) throw StateError('offline');
      acknowledgements.add(state);
    },
  );
  setUp(() {
    values = {'font_scale': 1.0, 'theme': 'system', 'visible': true};
    receipts = [];
    acknowledgements = [];
    applications = 0;
    offline = false;
  });

  test('concurrent replay and process restart apply once and retain the before-image', () async {
    final runner = executor();
    await Future.wait([runner.receive('m', action()), runner.receive('m', action())]);
    values['font_scale'] = 1.3; // A subsequent manual preference must survive replay.
    await executor().receive('m', action());
    expect(applications, 1);
    expect(values['font_scale'], 1.3);
    expect(acknowledgements.last['previous'], {'font_scale': 1.0});
  });

  test('undo restores previous values once and pending replay stays undone', () async {
    final runner = executor();
    await runner.receive('m', action(patch: {'font_scale': 1.3, 'theme': 'dark'}));
    await runner.undo('m', action());
    await runner.undo('m', action());
    await runner.receive('m', action());
    expect(values['font_scale'], 1.0);
    expect(values['theme'], 'system');
    expect(applications, 2);
    expect(acknowledgements.last['status'], 'undone');
  });

  test('other devices, applied history and unrelated components are ignored', () async {
    final runner = executor();
    await runner.receive('m', action(device: 'dev_other'));
    await runner.undo('m', action(device: 'dev_other'));
    await runner.receive('m', action(state: {'status': 'applied'}));
    await runner.receive('m', const MessageComponent(id: 'todo', kind: 'todo'));
    expect(applications, 0);
    expect(receipts, isEmpty);
    expect(acknowledgements, isEmpty);
  });

  test('only whitelisted keys and values execute; unknown actions are unsupported', () async {
    final runner = executor();
    await runner.receive('m', action(patch: {'font_scale': 1.15, 'theme': 'dark', 'logout': true, 'haptics': false, 'tts_rate': .3}));
    expect(values, {'font_scale': 1.15, 'theme': 'dark', 'visible': true});
    await runner.receive('m', action(id: 'bad', patch: {'font_scale': 99, 'theme': 'unknown'}));
    await runner.receive('m', action(id: 'page', kind: 'open_page', patch: {'page': 'settings'}));
    expect(applications, 1);
    expect(acknowledgements.last['status'], 'unsupported');
  });

  test('suggestions visibility applies and undoes through the same journal', () async {
    final runner = executor();
    final component = action(kind: 'toggle_suggestions', patch: {'visible': false, 'theme': 'dark'});
    await runner.receive('m', component);
    expect(values['visible'], false);
    expect(values['theme'], 'system');
    await runner.undo('m', component);
    expect(values['visible'], true);
  });

  test('failed acknowledgements retry without applying twice, including undo', () async {
    offline = true;
    await expectLater(executor().receive('m', action()), throwsStateError);
    offline = false;
    await executor().receive('m', action());
    expect(applications, 1);
    offline = true;
    await expectLater(executor().undo('m', action()), throwsStateError);
    offline = false;
    await executor().undo('m', action());
    expect(applications, 2);
    expect(acknowledgements.last['status'], 'undone');
  });

  test('prepared receipt recovers an interrupted absolute patch', () async {
    receipts = [
      {
        'id': 'a1',
        'phase': 'prepared',
        'patch': {'font_scale': 1.15},
        'state': {
          'status': 'applied',
          'applied_device_id': 'dev_this',
          'previous': {'font_scale': 1.0},
        },
      },
    ];
    await executor().receive('m', action());
    expect(values['font_scale'], 1.15);
    expect(receipts.single['phase'], 'applied');
  });

  test('journal keeps the most recent 200 receipts', () async {
    final runner = executor();
    for (var i = 0; i < 205; i++) {
      await runner.receive('m', action(id: 'a$i'));
    }
    expect(receipts.length, 200);
    expect(receipts.first['id'], 'a5');
  });
}
