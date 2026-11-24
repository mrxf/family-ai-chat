/// Shared font scales used by settings and conversation actions.
const fontSteps = [(0.85, '小'), (0.92, '较小'), (1.0, '标准'), (1.15, '大'), (1.3, '特大')];
final fontScaleSteps = List<double>.unmodifiable(fontSteps.map((step) => step.$1));
final fontScaleLabels = List<String>.unmodifiable(fontSteps.map((step) => step.$2));

/// Finds the closest supported font step for a saved scale.
int nearestFontStep(double value) {
  var best = 0;
  for (var i = 1; i < fontScaleSteps.length; i++) {
    if ((fontScaleSteps[i] - value).abs() < (fontScaleSteps[best] - value).abs()) best = i;
  }
  return best;
}
