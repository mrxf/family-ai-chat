import 'dart:typed_data';

/// An image chosen by the user and already prepared (re-encoded) for upload.
class PickedImage {
  final Uint8List bytes;

  /// Local copy used for instant previews; empty when it could not be written.
  final String path;
  final int width, height;
  const PickedImage({required this.bytes, required this.path, required this.width, required this.height});
}
