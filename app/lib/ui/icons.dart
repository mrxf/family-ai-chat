import 'package:flutter/widgets.dart';
import 'package:phosphoricons_flutter/phosphoricons_flutter.dart';

/// Unified slender-stroke icon set (Phosphor Light ~1.2dp / Regular ~1.5dp)
/// replacing Flutter's heavy 2.0dp+ Material glyphs across the entire app.
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
}
