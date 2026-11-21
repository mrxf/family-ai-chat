import 'package:flutter/material.dart';

import '../../../../core/models/models.dart';
import 'ask_user_card.dart';
import 'chart_card.dart';
import 'metrics_card.dart';
import 'quiz_card.dart';
import 'todo_card.dart';
import 'weather_card.dart';

/// Dispatcher for all supported assistant component kinds.
class ChatComponentCard extends StatelessWidget {
  const ChatComponentCard({
    super.key,
    required this.messageId,
    required this.component,
    required this.isLastMessage,
  });

  final String messageId;
  final MessageComponent component;
  final bool isLastMessage;

  @override
  Widget build(BuildContext context) {
    return switch (component.kind) {
      'weather' => WeatherCard(component: component),
      'todo' => TodoCard(messageId: messageId, component: component),
      'chart' => ChartCard(component: component),
      'quiz' => QuizCard(messageId: messageId, component: component),
      'metrics' => MetricsCard(component: component),
      'ask_user' => AskUserCard(
          messageId: messageId,
          component: component,
          isLastMessage: isLastMessage,
        ),
      _ => const SizedBox.shrink(),
    };
  }
}
