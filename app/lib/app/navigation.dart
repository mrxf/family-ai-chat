import 'package:flutter/widgets.dart';

/// The app's root navigator. Used by flows that start outside any widget
/// context (notification taps, content shared from other apps, logout).
final navigatorKey = GlobalKey<NavigatorState>();
