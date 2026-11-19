import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'icons.dart';
import 'theme.dart';

/// The 小暖 mascot. [thinking] swaps in the pondering pose.
class Mascot extends StatelessWidget {
  const Mascot({super.key, this.size = 40, this.thinking = false});
  final double size;
  final bool thinking;

  @override
  Widget build(BuildContext context) => Image.asset(
        thinking ? 'assets/brand/mascot_thinking.png' : 'assets/brand/mascot.png',
        width: size,
        height: size,
        filterQuality: FilterQuality.medium,
        gaplessPlayback: true,
      );
}

/// Gently bobs its child up and down — used for the mascot on empty states.
class Floating extends StatefulWidget {
  const Floating({super.key, required this.child, this.amplitude = 6, this.period = const Duration(milliseconds: 2600)});
  final Widget child;
  final double amplitude;
  final Duration period;

  @override
  State<Floating> createState() => _FloatingState();
}

class _FloatingState extends State<Floating> with SingleTickerProviderStateMixin {
  late final _c = AnimationController(vsync: this, duration: widget.period)..repeat(reverse: true);

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
        animation: _c,
        builder: (_, child) => Transform.translate(
          offset: Offset(0, -widget.amplitude * Curves.easeInOut.transform(_c.value)),
          child: child,
        ),
        child: widget.child,
      );
}

/// Scales down slightly while pressed. Gives every tappable surface a tactile feel.
class Pressable extends StatefulWidget {
  const Pressable({super.key, required this.child, this.onTap, this.onLongPress, this.scale = 0.96, this.haptic = true});
  final Widget child;
  final VoidCallback? onTap, onLongPress;
  final double scale;
  final bool haptic;

  @override
  State<Pressable> createState() => _PressableState();
}

class _PressableState extends State<Pressable> {
  bool _down = false;

  void _set(bool v) {
    if (_down != v) setState(() => _down = v);
  }

  @override
  Widget build(BuildContext context) {
    final enabled = widget.onTap != null || widget.onLongPress != null;
    return GestureDetector(
      behavior: HitTestBehavior.opaque,
      onTapDown: enabled ? (_) => _set(true) : null,
      onTapUp: enabled ? (_) => _set(false) : null,
      onTapCancel: enabled ? () => _set(false) : null,
      onTap: widget.onTap == null
          ? null
          : () {
              if (widget.haptic) HapticFeedback.selectionClick();
              widget.onTap!();
            },
      onLongPress: widget.onLongPress == null
          ? null
          : () {
              _set(false);
              HapticFeedback.mediumImpact();
              widget.onLongPress!();
            },
      child: AnimatedScale(
        scale: _down ? widget.scale : 1,
        duration: const Duration(milliseconds: 120),
        curve: Curves.easeOut,
        child: widget.child,
      ),
    );
  }
}

/// Round avatar showing an emoji or the first character of a name.
class Avatar extends StatelessWidget {
  const Avatar({super.key, required this.label, this.emoji = '', this.size = 40});
  final String label, emoji;
  final double size;

  static const _colors = [Color(0xFFFF9C5B), Color(0xFF5B9BFF), Color(0xFF4DBB8A), Color(0xFFB57BEA), Color(0xFFE8A23B), Color(0xFFEB6A93)];

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    if (emoji.isNotEmpty) {
      return Container(
        width: size,
        height: size,
        alignment: Alignment.center,
        decoration: BoxDecoration(color: p.accentSoft, shape: BoxShape.circle),
        child: Text(emoji, style: TextStyle(fontSize: size * 0.52, height: 1.1), textScaler: TextScaler.noScaling),
      );
    }
    final c = _colors[label.hashCode.abs() % _colors.length];
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(gradient: LinearGradient(colors: [c, Color.lerp(c, Colors.black, 0.15)!]), shape: BoxShape.circle),
      child: Text(label.isEmpty ? '?' : label.characters.first,
          style: TextStyle(color: Colors.white, fontSize: size * 0.42, fontWeight: FontWeight.w700), textScaler: TextScaler.noScaling),
    );
  }
}

/// Small circular icon button with a soft background, sized for older fingers (48dp target).
class SoftIconButton extends StatelessWidget {
  const SoftIconButton({super.key, required this.icon, this.onTap, this.tooltip, this.size = 44, this.filled = false, this.color});
  final IconData icon;
  final VoidCallback? onTap;
  final String? tooltip;
  final double size;
  final bool filled;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final btn = Pressable(
      onTap: onTap,
      child: Container(
        width: size,
        height: size,
        decoration: BoxDecoration(color: filled ? p.surface : Colors.transparent, shape: BoxShape.circle, boxShadow: filled ? [BoxShadow(color: p.shadow, blurRadius: 10, offset: const Offset(0, 2))] : null),
        child: AppIcon(icon, size: size * 0.52, color: color ?? p.text),
      ),
    );
    return tooltip == null ? btn : Tooltip(message: tooltip!, child: btn);
  }
}

void showToast(BuildContext context, String msg) {
  final m = ScaffoldMessenger.maybeOf(context);
  if (m == null) return;
  m
    ..hideCurrentSnackBar()
    ..showSnackBar(SnackBar(content: Text(msg), duration: const Duration(milliseconds: 2200)));
}
