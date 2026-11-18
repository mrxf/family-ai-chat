package com.nuan.chat

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Parcelable
import android.webkit.MimeTypeMap
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.io.FileOutputStream
import java.util.concurrent.Executors

class MainActivity : FlutterActivity() {
    companion object {
        private const val CHANNEL = "com.nuan.chat/share"
        private const val CONSUMED_EXTRA = "com.nuan.chat.SHARE_CONSUMED"
        private const val MAX_IMAGES = 9
        private const val STALE_MS = 60 * 60 * 1000L // 1 hour
    }

    private val copyExecutor = Executors.newSingleThreadExecutor()
    private var shareChannel: MethodChannel? = null
    private var dartReady = false
    private var pendingPayload: Map<String, Any>? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        handleIncomingIntent(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleIncomingIntent(intent)
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        val channel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, CHANNEL)
        channel.setMethodCallHandler { call, result ->
            when (call.method) {
                "takePendingShare" -> {
                    dartReady = true
                    val payload = pendingPayload
                    pendingPayload = null
                    result.success(payload)
                }
                else -> result.notImplemented()
            }
        }
        shareChannel = channel
    }

    override fun cleanUpFlutterEngine(flutterEngine: FlutterEngine) {
        shareChannel?.setMethodCallHandler(null)
        shareChannel = null
        dartReady = false
        super.cleanUpFlutterEngine(flutterEngine)
    }

    override fun onDestroy() {
        copyExecutor.shutdown()
        super.onDestroy()
    }

    private fun handleIncomingIntent(intent: Intent?) {
        if (intent == null) return
        if ((intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY) != 0) return
        if (intent.getBooleanExtra(CONSUMED_EXTRA, false)) return

        val action = intent.action ?: return
        val type = intent.type

        var text = ""
        val uris = mutableListOf<Uri>()

        when (action) {
            Intent.ACTION_PROCESS_TEXT -> {
                text = intent.getCharSequenceExtra(Intent.EXTRA_PROCESS_TEXT)?.toString()?.trim().orEmpty()
            }
            Intent.ACTION_SEND -> {
                if (type?.startsWith("image/") == true || intent.hasExtra(Intent.EXTRA_STREAM)) {
                    val stream = parcelableExtra<Uri>(intent, Intent.EXTRA_STREAM)
                        ?: intent.clipData?.takeIf { it.itemCount > 0 && type?.startsWith("image/") == true }?.getItemAt(0)?.uri
                    if (stream != null) uris.add(stream)
                }
                text = extractSharedText(intent, hasImages = uris.isNotEmpty())
            }
            Intent.ACTION_SEND_MULTIPLE -> {
                val streams = parcelableArrayListExtra<Uri>(intent, Intent.EXTRA_STREAM)
                if (!streams.isNullOrEmpty()) {
                    uris.addAll(streams)
                } else if (type == null || type.startsWith("image/") || type == "*/*") {
                    val clip = intent.clipData
                    if (clip != null) {
                        for (i in 0 until clip.itemCount) {
                            clip.getItemAt(i)?.uri?.let { uris.add(it) }
                        }
                    }
                }
                text = extractSharedText(intent, hasImages = uris.isNotEmpty())
            }
            Intent.ACTION_VIEW -> {
                val data = intent.data
                val resolvedMime = type ?: data?.let { contentResolver.getType(it) }
                if (data != null && resolvedMime?.startsWith("image/") == true) {
                    uris.add(data)
                }
            }
            else -> return
        }

        if (text.isEmpty() && uris.isEmpty()) return
        intent.putExtra(CONSUMED_EXTRA, true)

        if (uris.isEmpty()) {
            deliverPayload(
                mapOf(
                    "text" to text,
                    "images" to emptyList<String>(),
                    "totalImages" to 0,
                    "failedImages" to 0,
                )
            )
            return
        }

        val appCacheDir = applicationContext.cacheDir
        val resolver = applicationContext.contentResolver
        val totalImages = uris.size
        val toCopy = uris.take(MAX_IMAGES)

        copyExecutor.execute {
            val sharedDir = File(appCacheDir, "shared_incoming").apply { mkdirs() }
            sweepStaleFiles(sharedDir)

            val copiedPaths = ArrayList<String>(toCopy.size)
            var failed = 0
            val batchTs = System.currentTimeMillis()

            for ((index, uri) in toCopy.withIndex()) {
                val ext = MimeTypeMap.getSingleton()
                    .getExtensionFromMimeType(resolver.getType(uri))
                    ?.takeIf { it.isNotBlank() } ?: "img"
                val dest = File(sharedDir, "share_${batchTs}_$index.$ext")
                try {
                    val input = resolver.openInputStream(uri)
                    if (input == null) {
                        failed++
                        continue
                    }
                    input.use { stream ->
                        FileOutputStream(dest).use { out ->
                            stream.copyTo(out)
                        }
                    }
                    if (dest.length() > 0L) {
                        copiedPaths.add(dest.absolutePath)
                    } else {
                        dest.delete()
                        failed++
                    }
                } catch (_: Exception) {
                    dest.delete()
                    failed++
                }
            }

            val payload = mapOf<String, Any>(
                "text" to text,
                "images" to copiedPaths,
                "totalImages" to totalImages,
                "failedImages" to failed,
            )
            runOnUiThread {
                if (!isDestroyed) deliverPayload(payload)
            }
        }
    }

    private fun deliverPayload(payload: Map<String, Any>) {
        val channel = shareChannel
        if (dartReady && channel != null) {
            channel.invokeMethod("onShare", payload)
        } else {
            pendingPayload = payload
        }
    }

    private fun extractSharedText(intent: Intent, hasImages: Boolean): String {
        val rawText = intent.getCharSequenceExtra(Intent.EXTRA_TEXT)?.toString()?.trim().orEmpty()
        val subject = intent.getCharSequenceExtra(Intent.EXTRA_SUBJECT)?.toString()?.trim().orEmpty()
        // Some gallery apps place a content:// or file:// URI string inside EXTRA_TEXT alongside EXTRA_STREAM.
        val body = if (hasImages && (rawText.startsWith("content://") || rawText.startsWith("file://"))) "" else rawText
        return when {
            subject.isNotEmpty() && body.isNotEmpty() && !body.contains(subject) -> "$subject\n$body"
            body.isNotEmpty() -> body
            else -> subject
        }
    }

    private fun sweepStaleFiles(dir: File) {
        val cutoff = System.currentTimeMillis() - STALE_MS
        dir.listFiles()?.forEach { f ->
            if (f.isFile && f.lastModified() < cutoff) {
                f.delete()
            }
        }
    }

    private inline fun <reified T : Parcelable> parcelableExtra(intent: Intent, key: String): T? =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            intent.getParcelableExtra(key, T::class.java)
        } else {
            @Suppress("DEPRECATION")
            intent.getParcelableExtra(key)
        }

    private inline fun <reified T : Parcelable> parcelableArrayListExtra(intent: Intent, key: String): ArrayList<T>? =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            intent.getParcelableArrayListExtra(key, T::class.java)
        } else {
            @Suppress("DEPRECATION")
            intent.getParcelableArrayListExtra(key)
        }
}
