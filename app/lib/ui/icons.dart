import 'package:flutter/widgets.dart';
import 'package:phosphoricons_flutter/phosphoricons_flutter.dart';

typedef _IconSet = ({IconData thin, IconData light, IconData regular, IconData bold});

_IconSet _set(IconData thin, IconData light, IconData regular, IconData bold) =>
    (thin: thin, light: light, regular: regular, bold: bold);

/// Unified slender-stroke icon set (Phosphor Light ~1.2dp / Regular ~1.5dp)
/// replacing Flutter's heavy 2.0dp+ Material glyphs across the entire app.
///
/// Use [AppIcon] (or [AppIcons.resolve]) so icon stroke weight dynamically
/// tracks the user's text scale: thinner at small sizes, bolder at large sizes.
@staticIconProvider
abstract final class AppIcons {
  // ---- navigation & chrome ----
  static const IconData menu = PhosphorIconsLight.list;
  static const IconData back = PhosphorIconsLight.caretLeft;
  static const IconData close = PhosphorIconsLight.x;
  static const IconData closeSmall = PhosphorIconsRegular.x;
  static const IconData closeCircle = PhosphorIconsLight.xCircle;
  static const IconData chevronDown = PhosphorIconsLight.caretDown;
  static const IconData chevronUp = PhosphorIconsLight.caretUp;
  static const IconData chevronRight = PhosphorIconsLight.caretRight;
  static const IconData arrowDown = PhosphorIconsLight.arrowDown;
  static const IconData arrowUp = PhosphorIconsRegular.arrowUp;
  static const IconData arrowUpRight = PhosphorIconsLight.arrowUpRight;
  static const IconData externalLink = PhosphorIconsLight.arrowSquareOut;
  static const IconData plus = PhosphorIconsLight.plus;
  static const IconData more = PhosphorIconsLight.dotsThree;
  static const IconData dragHandle = PhosphorIconsLight.equals;

  // ---- chat & privacy ----
  static const IconData newChat = PhosphorIconsLight.notePencil;
  static const IconData share = PhosphorIconsLight.shareFat;
  static const IconData chatBubble = PhosphorIconsLight.chatCircle;
  static const IconData quotes = PhosphorIconsLight.quotes;
  static const IconData privateChat = PhosphorIconsLight.eyeSlash;
  static const IconData eye = PhosphorIconsLight.eye;
  static const IconData eyeOff = PhosphorIconsLight.eyeSlash;
  static const IconData search = PhosphorIconsLight.magnifyingGlass;
  static const IconData manageSearch = PhosphorIconsLight.listMagnifyingGlass;
  static const IconData globe = PhosphorIconsLight.globe;
  static const IconData article = PhosphorIconsLight.article;
  static const IconData pin = PhosphorIconsLight.pushPin;
  static const IconData unpin = PhosphorIconsLight.pushPinSlash;

  // ---- composer, media & message actions ----
  static const IconData camera = PhosphorIconsLight.camera;
  static const IconData image = PhosphorIconsLight.image;
  static const IconData brokenImage = PhosphorIconsLight.imageBroken;
  static const IconData thinkOff = PhosphorIconsLight.lightbulb;
  static const IconData thinkOn = PhosphorIconsRegular.lightbulbFilament;
  static const IconData mic = PhosphorIconsLight.microphone;
  static const IconData micOff = PhosphorIconsLight.microphoneSlash;
  static const IconData copy = PhosphorIconsLight.copy;
  static const IconData paste = PhosphorIconsLight.clipboardText;
  static const IconData edit = PhosphorIconsLight.pencilSimple;
  static const IconData editNote = PhosphorIconsLight.notePencil;
  static const IconData rename = PhosphorIconsLight.pencilSimpleLine;
  static const IconData delete = PhosphorIconsLight.trash;
  static const IconData clearAll = PhosphorIconsLight.broom;
  static const IconData history = PhosphorIconsLight.clockCounterClockwise;
  static const IconData download = PhosphorIconsLight.downloadSimple;
  static const IconData speaker = PhosphorIconsLight.speakerHigh;
  static const IconData stopCircle = PhosphorIconsLight.stopCircle;
  static const IconData refresh = PhosphorIconsLight.arrowsClockwise;
  static const IconData swap = PhosphorIconsLight.arrowsLeftRight;
  static const IconData selectText = PhosphorIconsLight.textT;

  // ---- settings, memories, auth & admin ----
  static const IconData settings = PhosphorIconsLight.gearSix;
  static const IconData tune = PhosphorIconsLight.slidersHorizontal;
  static const IconData heart = PhosphorIconsLight.heart;
  static const IconData inbox = PhosphorIconsLight.trayArrowDown;
  static const IconData sparkles = PhosphorIconsLight.sparkle;
  static const IconData magicWand = PhosphorIconsLight.magicWand;
  static const IconData palette = PhosphorIconsLight.palette;
  static const IconData repeat = PhosphorIconsLight.repeat;
  static const IconData user = PhosphorIconsLight.user;
  static const IconData userPlus = PhosphorIconsLight.userPlus;
  static const IconData family = PhosphorIconsLight.usersThree;
  static const IconData lock = PhosphorIconsLight.lockSimple;
  static const IconData lockOpen = PhosphorIconsLight.lockSimpleOpen;
  static const IconData key = PhosphorIconsLight.key;
  static const IconData pinCode = PhosphorIconsLight.hash;
  static const IconData shield = PhosphorIconsLight.shieldCheck;
  static const IconData shieldOff = PhosphorIconsLight.shieldSlash;
  static const IconData ban = PhosphorIconsLight.prohibit;
  static const IconData server = PhosphorIconsLight.hardDrives;
  static const IconData hub = PhosphorIconsLight.shareNetwork;
  static const IconData chart = PhosphorIconsLight.chartBar;
  static const IconData update = PhosphorIconsLight.arrowCircleUp;
  static const IconData info = PhosphorIconsLight.info;
  static const IconData error = PhosphorIconsLight.warningCircle;
  static const IconData cloudOff = PhosphorIconsLight.cloudSlash;
  static const IconData hourglass = PhosphorIconsLight.hourglassMedium;
  static const IconData rocket = PhosphorIconsLight.rocketLaunch;
  static const IconData check = PhosphorIconsLight.check;
  static const IconData checkBold = PhosphorIconsRegular.check;
  static const IconData checkCircle = PhosphorIconsRegular.checkCircle;
  static const IconData checkCircleFill = PhosphorIconsFill.checkCircle;
  static const IconData circle = PhosphorIconsLight.circle;
  static const IconData radioOn = PhosphorIconsRegular.radioButton;
  static const IconData radioOff = PhosphorIconsLight.circle;
  static const IconData star = PhosphorIconsLight.star;
  static const IconData starActive = PhosphorIconsRegular.star;
  static const IconData play = PhosphorIconsLight.play;
  static const IconData connectionTest = PhosphorIconsLight.broadcast;
  static const IconData badge = PhosphorIconsLight.identificationCard;
  static const IconData link = PhosphorIconsLight.linkSimple;
  static const IconData wallet = PhosphorIconsLight.wallet;
  static const IconData listPlus = PhosphorIconsLight.listPlus;

  // ---- provider emblems ----
  static const IconData route = PhosphorIconsLight.gitFork;
  static const IconData aperture = PhosphorIconsLight.aperture;
  static const IconData waves = PhosphorIconsLight.waves;
  static const IconData cloud = PhosphorIconsLight.cloud;
  static const IconData cpu = PhosphorIconsLight.cpu;
  static const IconData waveform = PhosphorIconsLight.waveform;

  /// Maps the effective text scale to a Phosphor stroke-weight step offset:
  /// - `< 0.90` (小 0.85) -> `-1` (Thin)
  /// - `0.90 ..< 1.10` (较小 0.92, 标准 1.0) -> `0` (Light / Regular)
  /// - `1.10 ..< 1.25` (大 1.15) -> `+1` (Regular / Bold)
  /// - `>= 1.25` (特大 1.3+) -> `+2` (Bold)
  static int weightDeltaForScale(double textScale) {
    if (textScale < 0.90) return -1;
    if (textScale < 1.10) return 0;
    if (textScale < 1.25) return 1;
    return 2;
  }

  /// Resolves [icon] to the Phosphor weight matching [context]'s text scale.
  static IconData resolve(BuildContext context, IconData icon) {
    final scale = MediaQuery.maybeTextScalerOf(context)?.scale(1.0) ?? 1.0;
    return resolveForScale(icon, scale);
  }

  /// Resolves [icon] to the Phosphor weight matching [textScale].
  static IconData resolveForScale(IconData icon, double textScale) {
    final delta = weightDeltaForScale(textScale);
    if (delta == 0) return icon;
    final entry = _lookup(icon);
    if (entry == null) return icon;
    final (icons, baseIndex) = entry;
    final target = (baseIndex + delta).clamp(0, 3);
    return switch (target) {
      0 => icons.thin,
      1 => icons.light,
      2 => icons.regular,
      _ => icons.bold,
    };
  }

  // Referencing the concrete Thin/Light/Regular/Bold constants here ensures
  // Flutter's release-mode `--tree-shake-icons` retains all four stroke weights.
  static (_IconSet, int)? _lookup(IconData icon) => switch (icon) {
        // Light-base icons (index 1)
        menu => (_set(PhosphorIconsThin.list, PhosphorIconsLight.list, PhosphorIconsRegular.list, PhosphorIconsBold.list), 1),
        back => (_set(PhosphorIconsThin.caretLeft, PhosphorIconsLight.caretLeft, PhosphorIconsRegular.caretLeft, PhosphorIconsBold.caretLeft), 1),
        close => (_set(PhosphorIconsThin.x, PhosphorIconsLight.x, PhosphorIconsRegular.x, PhosphorIconsBold.x), 1),
        closeCircle => (_set(PhosphorIconsThin.xCircle, PhosphorIconsLight.xCircle, PhosphorIconsRegular.xCircle, PhosphorIconsBold.xCircle), 1),
        chevronDown => (_set(PhosphorIconsThin.caretDown, PhosphorIconsLight.caretDown, PhosphorIconsRegular.caretDown, PhosphorIconsBold.caretDown), 1),
        chevronUp => (_set(PhosphorIconsThin.caretUp, PhosphorIconsLight.caretUp, PhosphorIconsRegular.caretUp, PhosphorIconsBold.caretUp), 1),
        chevronRight => (_set(PhosphorIconsThin.caretRight, PhosphorIconsLight.caretRight, PhosphorIconsRegular.caretRight, PhosphorIconsBold.caretRight), 1),
        arrowDown => (_set(PhosphorIconsThin.arrowDown, PhosphorIconsLight.arrowDown, PhosphorIconsRegular.arrowDown, PhosphorIconsBold.arrowDown), 1),
        arrowUpRight => (_set(PhosphorIconsThin.arrowUpRight, PhosphorIconsLight.arrowUpRight, PhosphorIconsRegular.arrowUpRight, PhosphorIconsBold.arrowUpRight), 1),
        externalLink => (_set(PhosphorIconsThin.arrowSquareOut, PhosphorIconsLight.arrowSquareOut, PhosphorIconsRegular.arrowSquareOut, PhosphorIconsBold.arrowSquareOut), 1),
        plus => (_set(PhosphorIconsThin.plus, PhosphorIconsLight.plus, PhosphorIconsRegular.plus, PhosphorIconsBold.plus), 1),
        more => (_set(PhosphorIconsThin.dotsThree, PhosphorIconsLight.dotsThree, PhosphorIconsRegular.dotsThree, PhosphorIconsBold.dotsThree), 1),
        dragHandle => (_set(PhosphorIconsThin.equals, PhosphorIconsLight.equals, PhosphorIconsRegular.equals, PhosphorIconsBold.equals), 1),
        newChat => (_set(PhosphorIconsThin.notePencil, PhosphorIconsLight.notePencil, PhosphorIconsRegular.notePencil, PhosphorIconsBold.notePencil), 1),
        share => (_set(PhosphorIconsThin.shareFat, PhosphorIconsLight.shareFat, PhosphorIconsRegular.shareFat, PhosphorIconsBold.shareFat), 1),
        chatBubble => (_set(PhosphorIconsThin.chatCircle, PhosphorIconsLight.chatCircle, PhosphorIconsRegular.chatCircle, PhosphorIconsBold.chatCircle), 1),
        quotes => (_set(PhosphorIconsThin.quotes, PhosphorIconsLight.quotes, PhosphorIconsRegular.quotes, PhosphorIconsBold.quotes), 1),
        privateChat => (_set(PhosphorIconsThin.eyeSlash, PhosphorIconsLight.eyeSlash, PhosphorIconsRegular.eyeSlash, PhosphorIconsBold.eyeSlash), 1),
        eye => (_set(PhosphorIconsThin.eye, PhosphorIconsLight.eye, PhosphorIconsRegular.eye, PhosphorIconsBold.eye), 1),
        search => (_set(PhosphorIconsThin.magnifyingGlass, PhosphorIconsLight.magnifyingGlass, PhosphorIconsRegular.magnifyingGlass, PhosphorIconsBold.magnifyingGlass), 1),
        manageSearch => (_set(PhosphorIconsThin.listMagnifyingGlass, PhosphorIconsLight.listMagnifyingGlass, PhosphorIconsRegular.listMagnifyingGlass, PhosphorIconsBold.listMagnifyingGlass), 1),
        globe => (_set(PhosphorIconsThin.globe, PhosphorIconsLight.globe, PhosphorIconsRegular.globe, PhosphorIconsBold.globe), 1),
        article => (_set(PhosphorIconsThin.article, PhosphorIconsLight.article, PhosphorIconsRegular.article, PhosphorIconsBold.article), 1),
        pin => (_set(PhosphorIconsThin.pushPin, PhosphorIconsLight.pushPin, PhosphorIconsRegular.pushPin, PhosphorIconsBold.pushPin), 1),
        unpin => (_set(PhosphorIconsThin.pushPinSlash, PhosphorIconsLight.pushPinSlash, PhosphorIconsRegular.pushPinSlash, PhosphorIconsBold.pushPinSlash), 1),
        camera => (_set(PhosphorIconsThin.camera, PhosphorIconsLight.camera, PhosphorIconsRegular.camera, PhosphorIconsBold.camera), 1),
        image => (_set(PhosphorIconsThin.image, PhosphorIconsLight.image, PhosphorIconsRegular.image, PhosphorIconsBold.image), 1),
        brokenImage => (_set(PhosphorIconsThin.imageBroken, PhosphorIconsLight.imageBroken, PhosphorIconsRegular.imageBroken, PhosphorIconsBold.imageBroken), 1),
        thinkOff => (_set(PhosphorIconsThin.lightbulb, PhosphorIconsLight.lightbulb, PhosphorIconsRegular.lightbulb, PhosphorIconsBold.lightbulb), 1),
        mic => (_set(PhosphorIconsThin.microphone, PhosphorIconsLight.microphone, PhosphorIconsRegular.microphone, PhosphorIconsBold.microphone), 1),
        micOff => (_set(PhosphorIconsThin.microphoneSlash, PhosphorIconsLight.microphoneSlash, PhosphorIconsRegular.microphoneSlash, PhosphorIconsBold.microphoneSlash), 1),
        copy => (_set(PhosphorIconsThin.copy, PhosphorIconsLight.copy, PhosphorIconsRegular.copy, PhosphorIconsBold.copy), 1),
        paste => (_set(PhosphorIconsThin.clipboardText, PhosphorIconsLight.clipboardText, PhosphorIconsRegular.clipboardText, PhosphorIconsBold.clipboardText), 1),
        edit => (_set(PhosphorIconsThin.pencilSimple, PhosphorIconsLight.pencilSimple, PhosphorIconsRegular.pencilSimple, PhosphorIconsBold.pencilSimple), 1),
        rename => (_set(PhosphorIconsThin.pencilSimpleLine, PhosphorIconsLight.pencilSimpleLine, PhosphorIconsRegular.pencilSimpleLine, PhosphorIconsBold.pencilSimpleLine), 1),
        delete => (_set(PhosphorIconsThin.trash, PhosphorIconsLight.trash, PhosphorIconsRegular.trash, PhosphorIconsBold.trash), 1),
        clearAll => (_set(PhosphorIconsThin.broom, PhosphorIconsLight.broom, PhosphorIconsRegular.broom, PhosphorIconsBold.broom), 1),
        history => (_set(PhosphorIconsThin.clockCounterClockwise, PhosphorIconsLight.clockCounterClockwise, PhosphorIconsRegular.clockCounterClockwise, PhosphorIconsBold.clockCounterClockwise), 1),
        download => (_set(PhosphorIconsThin.downloadSimple, PhosphorIconsLight.downloadSimple, PhosphorIconsRegular.downloadSimple, PhosphorIconsBold.downloadSimple), 1),
        speaker => (_set(PhosphorIconsThin.speakerHigh, PhosphorIconsLight.speakerHigh, PhosphorIconsRegular.speakerHigh, PhosphorIconsBold.speakerHigh), 1),
        stopCircle => (_set(PhosphorIconsThin.stopCircle, PhosphorIconsLight.stopCircle, PhosphorIconsRegular.stopCircle, PhosphorIconsBold.stopCircle), 1),
        refresh => (_set(PhosphorIconsThin.arrowsClockwise, PhosphorIconsLight.arrowsClockwise, PhosphorIconsRegular.arrowsClockwise, PhosphorIconsBold.arrowsClockwise), 1),
        swap => (_set(PhosphorIconsThin.arrowsLeftRight, PhosphorIconsLight.arrowsLeftRight, PhosphorIconsRegular.arrowsLeftRight, PhosphorIconsBold.arrowsLeftRight), 1),
        selectText => (_set(PhosphorIconsThin.textT, PhosphorIconsLight.textT, PhosphorIconsRegular.textT, PhosphorIconsBold.textT), 1),
        settings => (_set(PhosphorIconsThin.gearSix, PhosphorIconsLight.gearSix, PhosphorIconsRegular.gearSix, PhosphorIconsBold.gearSix), 1),
        tune => (_set(PhosphorIconsThin.slidersHorizontal, PhosphorIconsLight.slidersHorizontal, PhosphorIconsRegular.slidersHorizontal, PhosphorIconsBold.slidersHorizontal), 1),
        heart => (_set(PhosphorIconsThin.heart, PhosphorIconsLight.heart, PhosphorIconsRegular.heart, PhosphorIconsBold.heart), 1),
        inbox => (_set(PhosphorIconsThin.trayArrowDown, PhosphorIconsLight.trayArrowDown, PhosphorIconsRegular.trayArrowDown, PhosphorIconsBold.trayArrowDown), 1),
        sparkles => (_set(PhosphorIconsThin.sparkle, PhosphorIconsLight.sparkle, PhosphorIconsRegular.sparkle, PhosphorIconsBold.sparkle), 1),
        magicWand => (_set(PhosphorIconsThin.magicWand, PhosphorIconsLight.magicWand, PhosphorIconsRegular.magicWand, PhosphorIconsBold.magicWand), 1),
        palette => (_set(PhosphorIconsThin.palette, PhosphorIconsLight.palette, PhosphorIconsRegular.palette, PhosphorIconsBold.palette), 1),
        repeat => (_set(PhosphorIconsThin.repeat, PhosphorIconsLight.repeat, PhosphorIconsRegular.repeat, PhosphorIconsBold.repeat), 1),
        user => (_set(PhosphorIconsThin.user, PhosphorIconsLight.user, PhosphorIconsRegular.user, PhosphorIconsBold.user), 1),
        userPlus => (_set(PhosphorIconsThin.userPlus, PhosphorIconsLight.userPlus, PhosphorIconsRegular.userPlus, PhosphorIconsBold.userPlus), 1),
        family => (_set(PhosphorIconsThin.usersThree, PhosphorIconsLight.usersThree, PhosphorIconsRegular.usersThree, PhosphorIconsBold.usersThree), 1),
        lock => (_set(PhosphorIconsThin.lockSimple, PhosphorIconsLight.lockSimple, PhosphorIconsRegular.lockSimple, PhosphorIconsBold.lockSimple), 1),
        lockOpen => (_set(PhosphorIconsThin.lockSimpleOpen, PhosphorIconsLight.lockSimpleOpen, PhosphorIconsRegular.lockSimpleOpen, PhosphorIconsBold.lockSimpleOpen), 1),
        key => (_set(PhosphorIconsThin.key, PhosphorIconsLight.key, PhosphorIconsRegular.key, PhosphorIconsBold.key), 1),
        pinCode => (_set(PhosphorIconsThin.hash, PhosphorIconsLight.hash, PhosphorIconsRegular.hash, PhosphorIconsBold.hash), 1),
        shield => (_set(PhosphorIconsThin.shieldCheck, PhosphorIconsLight.shieldCheck, PhosphorIconsRegular.shieldCheck, PhosphorIconsBold.shieldCheck), 1),
        shieldOff => (_set(PhosphorIconsThin.shieldSlash, PhosphorIconsLight.shieldSlash, PhosphorIconsRegular.shieldSlash, PhosphorIconsBold.shieldSlash), 1),
        ban => (_set(PhosphorIconsThin.prohibit, PhosphorIconsLight.prohibit, PhosphorIconsRegular.prohibit, PhosphorIconsBold.prohibit), 1),
        server => (_set(PhosphorIconsThin.hardDrives, PhosphorIconsLight.hardDrives, PhosphorIconsRegular.hardDrives, PhosphorIconsBold.hardDrives), 1),
        hub => (_set(PhosphorIconsThin.shareNetwork, PhosphorIconsLight.shareNetwork, PhosphorIconsRegular.shareNetwork, PhosphorIconsBold.shareNetwork), 1),
        chart => (_set(PhosphorIconsThin.chartBar, PhosphorIconsLight.chartBar, PhosphorIconsRegular.chartBar, PhosphorIconsBold.chartBar), 1),
        update => (_set(PhosphorIconsThin.arrowCircleUp, PhosphorIconsLight.arrowCircleUp, PhosphorIconsRegular.arrowCircleUp, PhosphorIconsBold.arrowCircleUp), 1),
        info => (_set(PhosphorIconsThin.info, PhosphorIconsLight.info, PhosphorIconsRegular.info, PhosphorIconsBold.info), 1),
        error => (_set(PhosphorIconsThin.warningCircle, PhosphorIconsLight.warningCircle, PhosphorIconsRegular.warningCircle, PhosphorIconsBold.warningCircle), 1),
        cloudOff => (_set(PhosphorIconsThin.cloudSlash, PhosphorIconsLight.cloudSlash, PhosphorIconsRegular.cloudSlash, PhosphorIconsBold.cloudSlash), 1),
        hourglass => (_set(PhosphorIconsThin.hourglassMedium, PhosphorIconsLight.hourglassMedium, PhosphorIconsRegular.hourglassMedium, PhosphorIconsBold.hourglassMedium), 1),
        rocket => (_set(PhosphorIconsThin.rocketLaunch, PhosphorIconsLight.rocketLaunch, PhosphorIconsRegular.rocketLaunch, PhosphorIconsBold.rocketLaunch), 1),
        check => (_set(PhosphorIconsThin.check, PhosphorIconsLight.check, PhosphorIconsRegular.check, PhosphorIconsBold.check), 1),
        circle => (_set(PhosphorIconsThin.circle, PhosphorIconsLight.circle, PhosphorIconsRegular.circle, PhosphorIconsBold.circle), 1),
        star => (_set(PhosphorIconsThin.star, PhosphorIconsLight.star, PhosphorIconsRegular.star, PhosphorIconsBold.star), 1),
        play => (_set(PhosphorIconsThin.play, PhosphorIconsLight.play, PhosphorIconsRegular.play, PhosphorIconsBold.play), 1),
        connectionTest => (_set(PhosphorIconsThin.broadcast, PhosphorIconsLight.broadcast, PhosphorIconsRegular.broadcast, PhosphorIconsBold.broadcast), 1),
        badge => (_set(PhosphorIconsThin.identificationCard, PhosphorIconsLight.identificationCard, PhosphorIconsRegular.identificationCard, PhosphorIconsBold.identificationCard), 1),
        link => (_set(PhosphorIconsThin.linkSimple, PhosphorIconsLight.linkSimple, PhosphorIconsRegular.linkSimple, PhosphorIconsBold.linkSimple), 1),
        wallet => (_set(PhosphorIconsThin.wallet, PhosphorIconsLight.wallet, PhosphorIconsRegular.wallet, PhosphorIconsBold.wallet), 1),
        listPlus => (_set(PhosphorIconsThin.listPlus, PhosphorIconsLight.listPlus, PhosphorIconsRegular.listPlus, PhosphorIconsBold.listPlus), 1),
        route => (_set(PhosphorIconsThin.gitFork, PhosphorIconsLight.gitFork, PhosphorIconsRegular.gitFork, PhosphorIconsBold.gitFork), 1),
        aperture => (_set(PhosphorIconsThin.aperture, PhosphorIconsLight.aperture, PhosphorIconsRegular.aperture, PhosphorIconsBold.aperture), 1),
        waves => (_set(PhosphorIconsThin.waves, PhosphorIconsLight.waves, PhosphorIconsRegular.waves, PhosphorIconsBold.waves), 1),
        cloud => (_set(PhosphorIconsThin.cloud, PhosphorIconsLight.cloud, PhosphorIconsRegular.cloud, PhosphorIconsBold.cloud), 1),
        cpu => (_set(PhosphorIconsThin.cpu, PhosphorIconsLight.cpu, PhosphorIconsRegular.cpu, PhosphorIconsBold.cpu), 1),
        waveform => (_set(PhosphorIconsThin.waveform, PhosphorIconsLight.waveform, PhosphorIconsRegular.waveform, PhosphorIconsBold.waveform), 1),

        // Regular-base icons (index 2 — emphasized one weight step above Light)
        closeSmall => (_set(PhosphorIconsThin.x, PhosphorIconsLight.x, PhosphorIconsRegular.x, PhosphorIconsBold.x), 2),
        arrowUp => (_set(PhosphorIconsThin.arrowUp, PhosphorIconsLight.arrowUp, PhosphorIconsRegular.arrowUp, PhosphorIconsBold.arrowUp), 2),
        thinkOn => (_set(PhosphorIconsThin.lightbulbFilament, PhosphorIconsLight.lightbulbFilament, PhosphorIconsRegular.lightbulbFilament, PhosphorIconsBold.lightbulbFilament), 2),
        checkBold => (_set(PhosphorIconsThin.check, PhosphorIconsLight.check, PhosphorIconsRegular.check, PhosphorIconsBold.check), 2),
        checkCircle => (_set(PhosphorIconsThin.checkCircle, PhosphorIconsLight.checkCircle, PhosphorIconsRegular.checkCircle, PhosphorIconsBold.checkCircle), 2),
        radioOn => (_set(PhosphorIconsThin.radioButton, PhosphorIconsLight.radioButton, PhosphorIconsRegular.radioButton, PhosphorIconsBold.radioButton), 2),
        starActive => (_set(PhosphorIconsThin.star, PhosphorIconsLight.star, PhosphorIconsRegular.star, PhosphorIconsBold.star), 2),

        _ => null,
      };
}

/// Drop-in replacement for [Icon] whose stroke weight dynamically tracks the
/// user's text scale via [AppIcons.resolve].
class AppIcon extends StatelessWidget {
  const AppIcon(
    this.icon, {
    super.key,
    this.size,
    this.color,
    this.semanticLabel,
    this.textDirection,
  });

  final IconData? icon;
  final double? size;
  final Color? color;
  final String? semanticLabel;
  final TextDirection? textDirection;

  @override
  Widget build(BuildContext context) {
    final resolved = icon == null ? null : AppIcons.resolve(context, icon!);
    return Icon(
      resolved,
      size: size,
      color: color,
      semanticLabel: semanticLabel,
      textDirection: textDirection,
    );
  }
}
