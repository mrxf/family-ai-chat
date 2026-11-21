import 'dart:io';
import 'dart:typed_data';
import 'dart:ui' as ui;

import 'package:flutter_image_compress/flutter_image_compress.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';
import 'package:path_provider/path_provider.dart';

import '../../core/models/models.dart';
import '../platform_capabilities.dart';

/// Gets pictures into the app (camera, gallery / file chooser) and prepares
/// them for upload: re-encoded as JPEG with the long edge ≤ 2048 and EXIF
/// stripped where native compression exists, otherwise sent as-is (≤ 20MB).
class ImageInput {
  ImageInput(this._caps, {ImagePicker? picker}) : _picker = picker ?? ImagePicker();

  final PlatformCapabilities _caps;
  final ImagePicker _picker;

  static const maxRawBytes = 20 * 1024 * 1024;

  bool get canTakePhoto => _caps.canTakePhoto;

  /// File paths of up to [limit] pictures chosen from the gallery
  /// (phones) or a file dialog (desktop).
  Future<List<String>> pickFromGallery({required int limit}) async {
    if (limit <= 0) return const [];
    final files = await _picker.pickMultiImage(limit: limit);
    return [for (final f in files.take(limit)) f.path];
  }

  /// Path of a new photo, or null when the user cancelled.
  Future<String?> takePhoto() async {
    if (!canTakePhoto) return null;
    return (await _picker.pickImage(source: ImageSource.camera))?.path;
  }

  /// Re-encodes [path] for upload and keeps a private temp copy for previews.
  /// Returns null when the file is empty, unreadable or too large.
  Future<PickedImage?> prepare(String path, {bool sourceIsTemporary = false}) async {
    Uint8List? bytes;
    var ext = 'jpg';
    if (_caps.nativeImageCompression) {
      try {
        bytes = await FlutterImageCompress.compressWithFile(path, minWidth: 2048, minHeight: 2048, quality: 85, keepExif: false, format: CompressFormat.jpeg);
      } catch (_) {}
    }
    if (bytes == null) {
      // Formats the encoder can't handle (e.g. GIF stickers) or no native encoder.
      final raw = await File(path).readAsBytes();
      if (raw.isEmpty || raw.length > maxRawBytes) return null;
      bytes = raw;
      final dot = path.lastIndexOf('.');
      if (dot != -1 && dot < path.length - 1) ext = path.substring(dot + 1).toLowerCase();
    }
    var out = '';
    try {
      final dir = await getTemporaryDirectory();
      out = '${dir.path}/nuan_${DateTime.now().microsecondsSinceEpoch}.$ext';
      await File(out).writeAsBytes(bytes);
    } catch (_) {
      // Never point previews at a temp file that is about to be deleted.
      out = sourceIsTemporary ? '' : path;
    }
    final (w, h) = await _dimensions(bytes);
    return PickedImage(bytes: bytes, path: out, width: w, height: h);
  }

  static Future<(int, int)> _dimensions(Uint8List bytes) async {
    final codec = await ui.instantiateImageCodec(bytes);
    final frame = await codec.getNextFrame();
    final size = (frame.image.width, frame.image.height);
    frame.image.dispose();
    codec.dispose();
    return size;
  }

  /// Best-effort removal of a local file the app created.
  static void discard(String path) {
    if (path.isEmpty) return;
    File(path).delete().ignore();
  }
}

final imageInputProvider = Provider<ImageInput>((ref) => ImageInput(ref.watch(platformCapabilitiesProvider)));
