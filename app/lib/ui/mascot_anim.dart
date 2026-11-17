import 'dart:async';
import 'dart:ui' as ui;

import 'package:flutter/scheduler.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import 'mascot_sheets.dart';
import 'widgets.dart';

/// What 小暖 is doing.
enum MascotMotion { idle, thinking, welcome }

/// Where the character sits inside `assets/brand/mascot.png` (512×512): every
/// animation frame is placed so its rest pose lands exactly here, so swapping
/// between the static image and the animation never jumps.
const _restInStatic = Rect.fromLTRB(54 / 512, 17 / 512, 458 / 512, 495 / 512);

final Map<String, Future<ui.Image>> _cache = {};

Future<ui.Image> _load(String asset) => _cache.putIfAbsent(asset, () async {
  final data = await rootBundle.load(asset);
  final codec = await ui.instantiateImageCodec(data.buffer.asUint8List());
  return (await codec.getNextFrame()).image;
});

/// Start decoding the sheets shown right after launch so the first frames
/// don't fall back to the static image.
void precacheMascot() {
  for (final m in [MascotMotion.idle, MascotMotion.welcome]) {
    _load(mascotSheets[m]!.asset).ignore();
  }
}

bool _welcomedThisLaunch = false;

/// Tests run each case in its own zone, so a sheet cached by an earlier case would never resolve.
@visibleForTesting
void debugResetMascot() {
  _cache.clear();
  _welcomedThisLaunch = false;
}

/// Animated 小暖. Plays [motion] on a loop; with [greet] it first waves hello
/// once per app launch, then settles into [motion].
///
/// Occupies a [size]×[size] box exactly like [Mascot] — motion such as a hop
/// may paint slightly outside it. Falls back to the static image while
/// loading and when the system asks for reduced motion.
class AnimatedMascot extends StatefulWidget {
  const AnimatedMascot({super.key, this.size = 40, this.motion = MascotMotion.idle, this.greet = false});
  final double size;
  final MascotMotion motion;
  final bool greet;

  @override
  State<AnimatedMascot> createState() => _AnimatedMascotState();
}

class _AnimatedMascotState extends State<AnimatedMascot> with SingleTickerProviderStateMixin {
  late final Ticker _ticker = createTicker(_tick);
  late MascotMotion _playing;
  Duration _start = Duration.zero;
  Duration _now = Duration.zero;
  int _frame = 0;
  final Map<MascotMotion, ui.Image> _images = {};

  @visibleForTesting
  MascotMotion get debugPlaying => _playing;

  @override
  void initState() {
    super.initState();
    _playing = widget.motion;
    if (widget.greet && !_welcomedThisLaunch) {
      _welcomedThisLaunch = true;
      _playing = MascotMotion.welcome;
    }
    _ensure(_playing);
    if (_playing != widget.motion) _ensure(widget.motion);
  }

  @override
  void didUpdateWidget(AnimatedMascot old) {
    super.didUpdateWidget(old);
    if (old.motion != widget.motion && _playing != MascotMotion.welcome) _switchTo(widget.motion);
  }

  void _ensure(MascotMotion m) {
    if (_images.containsKey(m)) return;
    _load(mascotSheets[m]!.asset).then(
      (img) {
        if (!mounted) return;
        setState(() => _images[m] = img);
        if (m == _playing) _restart();
      },
      onError: (Object _) {
        // A greeting that can't load just skips to the regular motion.
        if (mounted && m == MascotMotion.welcome && _playing == m) setState(() => _switchTo(widget.motion));
      },
    );
  }

  void _switchTo(MascotMotion m) {
    _playing = m;
    _ensure(m);
    _restart();
  }

  void _restart() {
    _frame = 0;
    if (_ticker.isActive) {
      _start = _now;
    } else if (_images.containsKey(_playing)) {
      _start = _now = Duration.zero; // a (re)started ticker counts from zero
      _ticker.start();
    }
  }

  void _tick(Duration t) {
    _now = t;
    final sheet = mascotSheets[_playing]!;
    if (_playing == MascotMotion.welcome && t - _start >= sheet.duration) {
      setState(() => _switchTo(widget.motion));
      // The greeting plays once per launch; free its ~20 MB decoded sheet.
      _images.remove(MascotMotion.welcome);
      _cache.remove(sheet.asset)?.then((img) => img.dispose()).ignore();
      return;
    }
    final f = ((t - _start).inMicroseconds * sheet.fps ~/ 1000000) % sheet.frames;
    if (f != _frame) setState(() => _frame = f);
  }

  @override
  void dispose() {
    _ticker.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final img = _images[_playing];
    if (img == null || MediaQuery.disableAnimationsOf(context)) {
      if (_ticker.isActive) _ticker.stop();
      return Mascot(size: widget.size);
    }
    return SizedBox.square(
      dimension: widget.size,
      child: CustomPaint(painter: _FramePainter(img, mascotSheets[_playing]!, _frame)),
    );
  }
}

class _FramePainter extends CustomPainter {
  _FramePainter(this.image, this.sheet, this.frame);
  final ui.Image image;
  final MascotSheet sheet;
  final int frame;

  @override
  void paint(Canvas canvas, Size size) {
    final col = frame % sheet.cols, row = frame ~/ sheet.cols;
    final src = Rect.fromLTWH(col * sheet.frameW, row * sheet.frameH, sheet.frameW, sheet.frameH);
    // Map the frame so its rest-pose box lands where the static image draws it.
    final target = Rect.fromLTRB(
      _restInStatic.left * size.width,
      _restInStatic.top * size.height,
      _restInStatic.right * size.width,
      _restInStatic.bottom * size.height,
    );
    final scale = target.width / sheet.rest.width;
    final dst = Rect.fromLTWH(
      target.left - sheet.rest.left * scale,
      target.top - sheet.rest.top * scale,
      sheet.frameW * scale,
      sheet.frameH * scale,
    );
    canvas.drawImageRect(image, src, dst, Paint()..filterQuality = FilterQuality.medium);
  }

  @override
  bool shouldRepaint(_FramePainter old) => old.frame != frame || old.image != image || old.sheet != sheet;
}
