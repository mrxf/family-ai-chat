import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/services.dart' show MissingPluginException;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:gal/gal.dart';
import 'package:path_provider/path_provider.dart';

enum SaveImagesStatus { saved, permissionDenied, failed }

class SaveImagesResult {
  const SaveImagesResult(this.status, {this.count = 0});
  final SaveImagesStatus status;

  /// How many images were written.
  final int count;

  bool get ok => status == SaveImagesStatus.saved;
}

/// Saves pictures where the user keeps photos: the gallery on phones,
/// Photos / the Pictures folder on desktop. Falls back to the downloads
/// folder when the gallery plugin is unavailable on this platform.
abstract interface class ImageSaver {
  Future<SaveImagesResult> save(List<Uint8List> images, {String namePrefix = 'xiaonuan'});
}

final imageSaverProvider = Provider<ImageSaver>((_) => const GalleryImageSaver());

class GalleryImageSaver implements ImageSaver {
  const GalleryImageSaver();

  @override
  Future<SaveImagesResult> save(List<Uint8List> images, {String namePrefix = 'xiaonuan'}) async {
    if (images.isEmpty) return const SaveImagesResult(SaveImagesStatus.saved);
    final stamp = DateTime.now().millisecondsSinceEpoch;
    String nameOf(int i) => images.length == 1 ? '${namePrefix}_$stamp' : '${namePrefix}_${stamp}_$i';
    try {
      if (!await Gal.hasPermission() && !await Gal.requestPermission()) {
        return const SaveImagesResult(SaveImagesStatus.permissionDenied);
      }
      for (var i = 0; i < images.length; i++) {
        await Gal.putImageBytes(images[i], name: nameOf(i));
      }
      return SaveImagesResult(SaveImagesStatus.saved, count: images.length);
    } on GalException catch (e) {
      return SaveImagesResult(e.type == GalExceptionType.permissionDenied ? SaveImagesStatus.permissionDenied : SaveImagesStatus.failed);
    } on MissingPluginException {
      return _saveToFolder(images, nameOf);
    } on UnsupportedError {
      return _saveToFolder(images, nameOf);
    } catch (_) {
      return const SaveImagesResult(SaveImagesStatus.failed);
    }
  }

  Future<SaveImagesResult> _saveToFolder(List<Uint8List> images, String Function(int) nameOf) async {
    try {
      final dir = await getDownloadsDirectory() ?? await getApplicationDocumentsDirectory();
      for (var i = 0; i < images.length; i++) {
        await File('${dir.path}/${nameOf(i)}.${_ext(images[i])}').writeAsBytes(images[i]);
      }
      return SaveImagesResult(SaveImagesStatus.saved, count: images.length);
    } catch (_) {
      return const SaveImagesResult(SaveImagesStatus.failed);
    }
  }

  static String _ext(Uint8List b) {
    if (b.length > 4 && b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4E && b[3] == 0x47) return 'png';
    if (b.length > 3 && b[0] == 0x47 && b[1] == 0x49 && b[2] == 0x46) return 'gif';
    if (b.length > 12 && b[8] == 0x57 && b[9] == 0x45 && b[10] == 0x42 && b[11] == 0x50) return 'webp';
    return 'jpg';
  }
}
