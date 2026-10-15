package com.crosshare.data.repository

import android.content.ContentValues
import android.content.Context
import android.net.Uri
import android.provider.MediaStore
import java.io.File
import java.io.FileOutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject

/** Desktop catalogue and durable, verified downloads into the system gallery. */
class LibraryClient(private val context: Context) {
    private val prefs = context.getSharedPreferences("crosshare_server", Context.MODE_PRIVATE)

    data class Asset(
        val hash: String,
        val name: String,
        val size: Long,
        val kind: String,
        val date: String,
        val favorite: Boolean,
    )

    data class Album(val id: Long, val name: String, val count: Int)

    data class Delivery(
        val id: Long,
        val hash: String,
        val name: String,
        val status: String,
        val error: String,
        val albumId: Long = 0,
        val albumName: String = "",
    )

    val paired
        get() = !prefs.getString("paired_token", null).isNullOrBlank()

    fun base(): String {
        val host = prefs.getString("paired_computer_host", null) ?: error("请先在设置中配对电脑")
        val address = if (host.contains(":")) "[$host]" else host
        return "http://$address:${prefs.getInt("paired_computer_port", 8384)}"
    }

    fun token() = prefs.getString("paired_token", "") ?: ""

    fun imageURL(hash: String, original: Boolean = false) =
        base() + "/api/library/" + (if (original) "asset/" else "thumbnail/") + hash

    private fun connection(path: String): HttpURLConnection =
        (URL(base() + path).openConnection() as HttpURLConnection).apply {
            setRequestProperty("Authorization", "Bearer ${token()}")
            connectTimeout = 10000
            readTimeout = 60000
        }

    suspend fun request(path: String, body: JSONObject? = null): JSONObject =
        withContext(Dispatchers.IO) {
            val conn =
                connection("/api/library$path").apply {
                    connectTimeout = 3000
                    readTimeout = 15000
                }
            try {
                if (body != null) {
                    conn.requestMethod = "POST"
                    conn.doOutput = true
                    conn.setRequestProperty("Content-Type", "application/json")
                    conn.outputStream.use { it.write(body.toString().toByteArray()) }
                }
                check(conn.responseCode in 200..299) {
                    conn.errorStream?.bufferedReader()?.use { it.readText() }
                        ?: "电脑连接失败 ${conn.responseCode}"
                }
                JSONObject(conn.inputStream.bufferedReader().use { it.readText() })
            } finally {
                conn.disconnect()
            }
        }

    suspend fun assets(offset: Int = 0, album: Long = 0, limit: Int = 48): List<Asset> {
        val a = request("?offset=$offset&album=$album&limit=$limit").getJSONArray("items")
        return (0 until a.length()).map { i ->
            a.getJSONObject(i).let {
                Asset(
                    it.getString("hash"),
                    it.getString("name"),
                    it.getLong("size"),
                    it.getString("kind"),
                    it.getString("date"),
                    it.getBoolean("favorite"),
                )
            }
        }
    }

    suspend fun albums(): List<Album> {
        val a = request("/albums").getJSONArray("items")
        return (0 until a.length()).map { i ->
            a.getJSONObject(i).let {
                Album(it.getLong("id"), it.getString("name"), it.getInt("count"))
            }
        }
    }

    suspend fun deliveries(
        offset: Int = 0,
        pending: Boolean = false,
        limit: Int = 48,
    ): List<Delivery> {
        val a =
            request("/transfers?offset=$offset&limit=$limit&pending=${if(pending)1 else 0}")
                .getJSONArray("items")
        return (0 until a.length()).map { i ->
            a.getJSONObject(i).let {
                Delivery(
                    it.getLong("id"),
                    it.getString("hash"),
                    it.getString("name"),
                    it.getString("status"),
                    it.getString("error"),
                    it.optLong("album_id", 0),
                    it.optString("album_name", ""),
                )
            }
        }
    }

    suspend fun queue(hash: String) {
        request("/send", JSONObject().put("hash", hash))
    }

    suspend fun queueAlbum(id: Long): Int =
        request("/albums/$id/send", JSONObject()).getInt("queued")

    suspend fun favorite(hash: String, value: Boolean) {
        request("/favorite/$hash", JSONObject().put("favorite", value))
    }

    private fun safeAlbumFolder(name: String): String =
        name
            .map { if (it == '/' || it == '\\' || it.code < 32 || it.code == 127) '_' else it }
            .joinToString("")
            .trim()
            .trim('.')
            .take(80)
            .ifBlank { "相册" }

    suspend fun receive(task: Delivery) =
        withContext(Dispatchers.IO) {
            require(task.hash.matches(Regex("[a-f0-9]{64}")))
            val resolver = context.contentResolver
            val scopeKey = task.hash + if (task.albumId > 0) "_album${task.albumId}" else ""
            val receiptKey = "received_$scopeKey"
            val indexed = MediaRepository(context).getByHash(task.hash)
            val indexedUri =
                indexed?.let {
                    android.content.ContentUris.withAppendedId(
                        if (it.mediaType == "video") MediaStore.Video.Media.EXTERNAL_CONTENT_URI
                        else MediaStore.Images.Media.EXTERNAL_CONTENT_URI,
                        it.mediaStoreId,
                    )
                }
            val previous = prefs.getString(receiptKey, null)?.let(Uri::parse)
            fun verified(uri: Uri): Boolean =
                try {
                    resolver.openInputStream(uri)?.use { input ->
                        val digest = MessageDigest.getInstance("SHA-256")
                        val buf = ByteArray(256 * 1024)
                        while (true) {
                            val n = input.read(buf)
                            if (n < 0) break
                            digest.update(buf, 0, n)
                        }
                        digest.digest().joinToString("") { "%02x".format(it) } == task.hash
                    } ?: false
                } catch (_: Exception) {
                    false
                }
            if (previous != null && verified(previous)) {
                if (prefs.contains("pending_$scopeKey")) {
                    check(
                        resolver.update(
                            previous,
                            ContentValues().apply { put(MediaStore.MediaColumns.IS_PENDING, 0) },
                            null,
                            null,
                        ) > 0
                    ) {
                        "无法发布到系统相册"
                    }
                    prefs.edit().remove("pending_$scopeKey").commit()
                }
                request("/transfers/${task.id}/ack", JSONObject())
                return@withContext
            }
            val localAvailable = indexedUri != null && verified(indexedUri)
            if (localAvailable && task.albumId == 0L) {
                request("/transfers/${task.id}/ack", JSONObject())
                return@withContext
            }
            // App-private partial file survives process death. The server supports Range.
            val dir = File(context.filesDir, "downloads").apply { mkdirs() }
            val part = File(dir, task.hash + ".part")
            if (localAvailable) {
                resolver.openInputStream(indexedUri!!)?.use { input ->
                    FileOutputStream(part).use { output ->
                        input.copyTo(output, 256 * 1024)
                        output.fd.sync()
                    }
                } ?: error("无法读取已有图片")
            } else {
                val conn = connection("/api/library/asset/${task.hash}")
                if (part.exists() && part.length() > 0)
                    conn.setRequestProperty("Range", "bytes=${part.length()}-")
                try {
                    val code = conn.responseCode
                    check(code == 200 || code == 206 || (code == 416 && part.exists())) {
                        "下载失败 $code"
                    }
                    if (code != 416) {
                        val append = code == 206
                        if (append)
                            check(
                                conn
                                    .getHeaderField("Content-Range")
                                    ?.startsWith("bytes ${part.length()}-") == true
                            ) {
                                "续传位置不匹配"
                            }
                        conn.inputStream.use { source ->
                            FileOutputStream(part, append).use { out ->
                                source.copyTo(out, 256 * 1024)
                                out.fd.sync()
                            }
                        }
                    }
                } finally {
                    conn.disconnect()
                }
            }
            val digest = MessageDigest.getInstance("SHA-256")
            part.inputStream().use { input ->
                val buf = ByteArray(256 * 1024)
                while (true) {
                    val n = input.read(buf)
                    if (n < 0) break
                    digest.update(buf, 0, n)
                }
            }
            val actual = digest.digest().joinToString("") { "%02x".format(it) }
            if (actual != task.hash) {
                part.delete()
                error("原图校验失败，请重试")
            }
            // Keep pending MediaStore URI in a journal before copying, so retries clean
            // up interrupted writes instead of publishing a second gallery image.
            val pendingKey = "pending_$scopeKey"
            prefs.getString(pendingKey, null)?.let {
                runCatching { resolver.delete(Uri.parse(it), null, null) }
            }
            val name =
                task.name.substringAfterLast('/').substringAfterLast('\\').ifBlank {
                    "${task.hash}.jpg"
                }
            val extension = name.substringAfterLast('.', "").lowercase()
            val mime =
                android.webkit.MimeTypeMap.getSingleton().getMimeTypeFromExtension(extension)
                    ?: "image/jpeg"
            val video = mime.startsWith("video/")
            val values =
                ContentValues().apply {
                    put(MediaStore.MediaColumns.DISPLAY_NAME, name)
                    put(MediaStore.MediaColumns.MIME_TYPE, mime)
                    put(
                        MediaStore.MediaColumns.RELATIVE_PATH,
                        (if (video) "Movies/CrossHare" else "Pictures/CrossHare") +
                            if (task.albumId > 0) "/" + safeAlbumFolder(task.albumName) else "",
                    )
                    put(MediaStore.MediaColumns.IS_PENDING, 1)
                }
            val collection =
                if (video) MediaStore.Video.Media.EXTERNAL_CONTENT_URI
                else MediaStore.Images.Media.EXTERNAL_CONTENT_URI
            val uri = resolver.insert(collection, values) ?: error("无法保存到系统相册")
            if (!prefs.edit().putString(pendingKey, uri.toString()).commit()) {
                resolver.delete(uri, null, null)
                error("无法保存接收进度")
            }
            try {
                resolver.openOutputStream(uri)?.use { out ->
                    part.inputStream().use { it.copyTo(out, 256 * 1024) }
                } ?: error("无法写入系统相册")
                check(verified(uri)) { "保存后的文件校验失败" }
                check(prefs.edit().putString(receiptKey, uri.toString()).commit()) { "无法保存接收回执" }
                check(
                    resolver.update(
                        uri,
                        ContentValues().apply { put(MediaStore.MediaColumns.IS_PENDING, 0) },
                        null,
                        null,
                    ) > 0
                ) {
                    "无法发布到系统相册"
                }
                prefs.edit().remove(pendingKey).commit()
                part.delete()
            } catch (e: Exception) {
                resolver.delete(uri, null, null)
                prefs.edit().remove(receiptKey).remove(pendingKey).commit()
                throw e
            }
            request("/transfers/${task.id}/ack", JSONObject())
        }
}
