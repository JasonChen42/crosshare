package com.crosshare.server

import android.content.Context
import android.content.Intent
import android.util.Log
import com.crosshare.data.db.AppDatabase
import com.crosshare.data.db.MediaEntity
import com.crosshare.data.repository.MediaRepository
import fi.iki.elonen.NanoHTTPD
import kotlinx.coroutines.runBlocking
import org.json.JSONArray
import org.json.JSONObject
import java.io.InputStream
import java.text.SimpleDateFormat
import java.util.*

/**
 * Embedded HTTP server that serves media files to the desktop client.
 *
 * Endpoints:
 * - GET /api/status -> device info
 * - POST /api/pair -> accept pairing
 * - GET /api/media/list -> all media items with hashes
 * - GET /api/media/download/{hash} -> download file (supports Range)
 */
class MediaServer(
    private val context: Context,
    private val repository: MediaRepository,
    port: Int = 8385
) : NanoHTTPD(port) {

    companion object {
        private const val TAG = "MediaServer"
        const val MIME_JSON = "application/json"
    }

    // Token management (stored in SharedPreferences)
    private val prefs by lazy {
        context.getSharedPreferences("crosshare_server", Context.MODE_PRIVATE)
    }

    private val deviceId: String
        get() = prefs.getString("device_id", null) ?: run {
            val id = UUID.randomUUID().toString()
            prefs.edit().putString("device_id", id).apply()
            id
        }

    private val deviceName: String
        get() = android.os.Build.MODEL

    /**
     * Get the token for a paired computer, or null if not paired.
     */
    fun getPairedToken(): String? = prefs.getString("paired_token", null)
    fun getPairedComputerName(): String? = prefs.getString("paired_computer_name", null)
    fun isPaired(): Boolean = getPairedToken() != null

    override fun serve(session: IHTTPSession): Response {
        val uri = session.uri
        val method = session.method

        Log.d(TAG, "${method.name} $uri")

        return try {
            when {
                uri == "/api/status" && method == Method.GET -> handleStatus()
                uri == "/api/pair" && method == Method.POST -> handlePair(session)
                uri == "/api/media/list" && method == Method.GET -> handleMediaList(session)
                uri == "/api/media/synced" && method == Method.POST -> handleSynced(session)
                uri.startsWith("/api/media/download/") && method == Method.GET -> handleDownload(session, uri)
                else -> newFixedLengthResponse(Response.Status.NOT_FOUND, MIME_JSON,
                    """{"error":"not found"}""")
            }
        } catch (e: Exception) {
            Log.e(TAG, "Error handling $uri: ${e.message}", e)
            newFixedLengthResponse(Response.Status.INTERNAL_ERROR, MIME_JSON,
                """{"error":"${e.message}"}""")
        }
    }

    private fun handleStatus(): Response {
        val json = JSONObject().apply {
            put("device_id", deviceId)
            put("device_name", deviceName)
            put("api_version", "1")
            put("paired", isPaired())
            put("total_media", runBlocking { repository.getTotalCount() })
        }
        return newFixedLengthResponse(Response.Status.OK, MIME_JSON, json.toString())
    }

    private fun handlePair(session: IHTTPSession): Response {
        // Read body
        val bodyMap = HashMap<String, String>()
        session.parseBody(bodyMap)
        val body = bodyMap["postData"] ?: ""

        val request = JSONObject(body)
        val pairCode = request.optString("pair_code", "")
        val computerName = request.optString("device_name", "Unknown PC")
        val computerToken = request.optString("token", "")

        if (computerToken.isEmpty()) {
            return newFixedLengthResponse(Response.Status.BAD_REQUEST, MIME_JSON,
                """{"error":"token is required"}""")
        }

        // Store the token from the computer
        prefs.edit()
            .putString("paired_token", computerToken)
            .putString("paired_computer_name", computerName)
            .apply()

        Log.i(TAG, "Paired with computer: $computerName")

        val json = JSONObject().apply {
            put("device_id", deviceId)
            put("device_name", deviceName)
            put("message", "paired successfully")
        }
        return newFixedLengthResponse(Response.Status.OK, MIME_JSON, json.toString())
    }

    private fun handleMediaList(session: IHTTPSession): Response {
        // Verify auth token
        if (!verifyAuth(session)) {
            return newFixedLengthResponse(Response.Status.UNAUTHORIZED, MIME_JSON,
                """{"error":"unauthorized"}""")
        }


        val dao = AppDatabase.getInstance(context).mediaDao()
        val mediaItems = runBlocking {
            // Use a simpler approach - get all via the hash list and entity lookup
            val hashes = dao.getAllHashes()
            hashes.mapNotNull { hash -> dao.getByHash(hash) }
        }

        val dateFormat = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ssZ", Locale.US)

        val itemsArray = JSONArray()
        for (item in mediaItems) {
            itemsArray.put(JSONObject().apply {
                put("content_hash", item.contentHash)
                put("file_name", item.fileName)
                put("file_size", item.fileSize)
                put("media_type", item.mediaType)
                put("mime_type", item.mimeType)
                put("date_taken", dateFormat.format(Date(item.dateTaken)))
            })
        }

        val json = JSONObject().apply {
            put("device_id", deviceId)
            put("device_name", deviceName)
            put("items", itemsArray)
            put("total_count", mediaItems.size)
        }

        return newFixedLengthResponse(Response.Status.OK, MIME_JSON, json.toString())
    }

    private fun handleSynced(session: IHTTPSession): Response {
        if (!verifyAuth(session)) {
            return newFixedLengthResponse(Response.Status.UNAUTHORIZED, MIME_JSON, """{"error":"unauthorized"}""")
        }
        val bodyMap = HashMap<String, String>()
        session.parseBody(bodyMap)
        val body = bodyMap["postData"] ?: ""
        
        try {
            val json = JSONObject(body)
            val hashesArray = json.optJSONArray("hashes") ?: JSONArray()
            val hashes = mutableListOf<String>()
            for (i in 0 until hashesArray.length()) {
                hashes.add(hashesArray.getString(i))
            }
            
            runBlocking {
                val dao = AppDatabase.getInstance(context).mediaDao()
                hashes.forEach { hash ->
                    dao.markSynced(hash)
                }
            }
            
            // Notify SyncService to refresh stats
            val intent = Intent(context, com.crosshare.service.SyncService::class.java).apply {
                action = "com.crosshare.ACTION_RESCAN"
            }
            context.startService(intent)

            return newFixedLengthResponse(Response.Status.OK, MIME_JSON, """{"success":true}""")
        } catch (e: Exception) {
            Log.e(TAG, "Error in handleSynced: ${e.message}", e)
            return newFixedLengthResponse(Response.Status.BAD_REQUEST, MIME_JSON, """{"error":"${e.message}"}""")
        }
    }

    private fun handleDownload(session: IHTTPSession, uri: String): Response {
        // Verify auth token
        if (!verifyAuth(session)) {
            return newFixedLengthResponse(Response.Status.UNAUTHORIZED, MIME_JSON,
                """{"error":"unauthorized"}""")
        }

        val hash = uri.removePrefix("/api/media/download/")
        if (hash.isEmpty()) {
            return newFixedLengthResponse(Response.Status.BAD_REQUEST, MIME_JSON,
                """{"error":"hash required"}""")
        }

        val fileSize = runBlocking { repository.getFileSize(hash) }
        if (fileSize == 0L) {
            return newFixedLengthResponse(Response.Status.NOT_FOUND, MIME_JSON,
                """{"error":"file not found"}""")
        }

        // Check for Range header (resume support)
        val rangeHeader = session.headers["range"]
        val entity = runBlocking { repository.getByHash(hash) }
            ?: return newFixedLengthResponse(Response.Status.NOT_FOUND, MIME_JSON,
                """{"error":"file not found"}""")

        val mimeType = entity.mimeType

        if (rangeHeader != null && rangeHeader.startsWith("bytes=")) {
            // Partial content (resume)
            return handleRangeDownload(hash, rangeHeader, fileSize, mimeType)
        }

        // Full download
        val inputStream = runBlocking { repository.openInputStream(hash) }
            ?: return newFixedLengthResponse(Response.Status.INTERNAL_ERROR, MIME_JSON,
                """{"error":"cannot read file"}""")

        val response = newFixedLengthResponse(Response.Status.OK, mimeType, inputStream, fileSize)
        response.addHeader("Accept-Ranges", "bytes")
        response.addHeader("Content-Length", fileSize.toString())
        response.addHeader("Content-Disposition", "attachment; filename=\"${entity.fileName}\"")
        return response
    }

    private fun handleRangeDownload(
        hash: String, rangeHeader: String, totalSize: Long, mimeType: String
    ): Response {
        // Parse "bytes=START-" or "bytes=START-END"
        val rangeSpec = rangeHeader.removePrefix("bytes=")
        val parts = rangeSpec.split("-")
        val start = parts[0].toLongOrNull() ?: 0
        val end = if (parts.size > 1 && parts[1].isNotEmpty()) {
            parts[1].toLongOrNull() ?: (totalSize - 1)
        } else {
            totalSize - 1
        }

        if (start >= totalSize) {
            return newFixedLengthResponse(
                Response.Status.RANGE_NOT_SATISFIABLE, MIME_JSON,
                """{"error":"range not satisfiable"}"""
            )
        }

        val inputStream = runBlocking { repository.openInputStream(hash) }
            ?: return newFixedLengthResponse(Response.Status.INTERNAL_ERROR, MIME_JSON,
                """{"error":"cannot read file"}""")

        // Skip to the start position
        inputStream.skip(start)

        val contentLength = end - start + 1

        val response = newFixedLengthResponse(
            Response.Status.PARTIAL_CONTENT, mimeType, inputStream, contentLength
        )
        response.addHeader("Content-Range", "bytes $start-$end/$totalSize")
        response.addHeader("Accept-Ranges", "bytes")
        response.addHeader("Content-Length", contentLength.toString())
        return response
    }

    private fun verifyAuth(session: IHTTPSession): Boolean {
        val token = getPairedToken() ?: return false // Not paired yet, reject
        val authHeader = session.headers["authorization"] ?: return false
        val bearerToken = if (authHeader.startsWith("Bearer ", ignoreCase = true)) {
            authHeader.substring(7)
        } else {
            authHeader
        }
        return bearerToken == token
    }

}
