package com.crosshare.data.repository

import android.annotation.SuppressLint
import android.content.ContentUris
import android.content.Context
import android.net.Uri
import android.provider.MediaStore
import android.util.Log
import com.crosshare.data.db.AppDatabase
import com.crosshare.data.db.MediaDao
import com.crosshare.data.db.MediaEntity
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.withContext
import java.io.InputStream
import java.security.MessageDigest

/**
 * Repository that scans MediaStore for photos/videos and computes SHA-256 hashes.
 */
class MediaRepository(private val context: Context) {

    private val dao: MediaDao = AppDatabase.getInstance(context).mediaDao()

    companion object {
        private const val TAG = "MediaRepository"
    }

    fun getAllMedia(): Flow<List<MediaEntity>> = dao.getAllMedia()
    fun getUnsyncedMedia(): Flow<List<MediaEntity>> = dao.getUnsyncedMedia()
    suspend fun getUnsyncedItems(): List<MediaEntity> = dao.getUnsyncedItems()

    suspend fun getAllHashes(): List<String> = dao.getAllHashes()
    suspend fun getTotalCount(): Int = dao.getTotalCount()
    suspend fun getSyncedCount(): Int = dao.getSyncedCount()
    suspend fun getSyncedSize(): Long = dao.getSyncedSize()
    suspend fun getByHash(hash: String): MediaEntity? = dao.getByHash(hash)
    suspend fun markSynced(hash: String) = dao.markSynced(hash)
    suspend fun markSyncedBatch(hashes: List<String>) = dao.markSyncedBatch(hashes)

    /**
     * Scans MediaStore for all photos and videos, computes SHA-256 hashes for new ones,
     * and inserts them into the local database.
     * Returns the number of newly indexed items.
     */
    suspend fun scanAndIndex(): Int = withContext(Dispatchers.IO) {
        var newCount = 0

        // Scan photos
        newCount += scanMedia(
            uri = MediaStore.Images.Media.EXTERNAL_CONTENT_URI,
            mediaType = "photo"
        )

        // Scan videos
        newCount += scanMedia(
            uri = MediaStore.Video.Media.EXTERNAL_CONTENT_URI,
            mediaType = "video"
        )

        Log.i(TAG, "Scan complete: $newCount new items indexed")
        newCount
    }

    private suspend fun scanMedia(uri: Uri, mediaType: String): Int {
        var newCount = 0

        val projection = arrayOf(
            MediaStore.MediaColumns._ID,
            MediaStore.MediaColumns.DISPLAY_NAME,
            MediaStore.MediaColumns.DATA,
            MediaStore.MediaColumns.SIZE,
            MediaStore.MediaColumns.MIME_TYPE,
            MediaStore.MediaColumns.DATE_TAKEN,
            MediaStore.MediaColumns.DATE_MODIFIED,
        )

        val sortOrder = "${MediaStore.MediaColumns.DATE_TAKEN} DESC"

        context.contentResolver.query(uri, projection, null, null, sortOrder)?.use { cursor ->
            val idCol = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns._ID)
            val nameCol = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns.DISPLAY_NAME)
            val dataCol = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns.DATA)
            val sizeCol = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns.SIZE)
            val mimeCol = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns.MIME_TYPE)
            val dateTakenCol = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns.DATE_TAKEN)
            val dateModCol = cursor.getColumnIndexOrThrow(MediaStore.MediaColumns.DATE_MODIFIED)

            while (cursor.moveToNext()) {
                val mediaStoreId = cursor.getLong(idCol)

                // Skip if already indexed
                if (dao.getByMediaStoreId(mediaStoreId) != null) continue

                val fileName = cursor.getString(nameCol) ?: "unknown"
                val filePath = cursor.getString(dataCol) ?: ""
                val fileSize = cursor.getLong(sizeCol)
                val mimeType = cursor.getString(mimeCol) ?: ""
                var dateTaken = cursor.getLong(dateTakenCol)
                if (dateTaken == 0L) {
                    dateTaken = cursor.getLong(dateModCol) * 1000 // DATE_MODIFIED is seconds
                }

                // Compute SHA-256 hash
                val contentUri = ContentUris.withAppendedId(uri, mediaStoreId)
                val hash = computeHash(contentUri) ?: continue

                val entity = MediaEntity(
                    mediaStoreId = mediaStoreId,
                    contentHash = hash,
                    fileName = fileName,
                    filePath = filePath,
                    fileSize = fileSize,
                    mimeType = mimeType,
                    mediaType = mediaType,
                    dateTaken = dateTaken
                )

                dao.insert(entity)
                newCount++

                if (newCount % 50 == 0) {
                    Log.d(TAG, "Indexed $newCount $mediaType items so far...")
                }
            }
        }

        return newCount
    }

    /**
     * Computes SHA-256 hash of a content URI.
     */
    private fun computeHash(contentUri: Uri): String? {
        return try {
            val digest = MessageDigest.getInstance("SHA-256")
            val buffer = ByteArray(8192)

            context.contentResolver.openInputStream(contentUri)?.use { inputStream: InputStream ->
                var bytesRead: Int
                while (inputStream.read(buffer).also { bytesRead = it } != -1) {
                    digest.update(buffer, 0, bytesRead)
                }
            } ?: return null

            digest.digest().joinToString("") { "%02x".format(it) }
        } catch (e: Exception) {
            Log.w(TAG, "Failed to hash $contentUri: ${e.message}")
            null
        }
    }

    /**
     * Opens an InputStream for the given content hash.
     * Used by the HTTP server to serve files to the desktop.
     */
    @SuppressLint("Recycle")
    suspend fun openInputStream(hash: String): InputStream? = withContext(Dispatchers.IO) {
        val entity = dao.getByHash(hash) ?: return@withContext null

        val uri = when (entity.mediaType) {
            "photo" -> ContentUris.withAppendedId(
                MediaStore.Images.Media.EXTERNAL_CONTENT_URI,
                entity.mediaStoreId
            )
            "video" -> ContentUris.withAppendedId(
                MediaStore.Video.Media.EXTERNAL_CONTENT_URI,
                entity.mediaStoreId
            )
            else -> return@withContext null
        }

        try {
            context.contentResolver.openInputStream(uri)
        } catch (e: Exception) {
            Log.w(TAG, "Failed to open $uri: ${e.message}")
            null
        }
    }

    /**
     * Returns the file size for a given hash.
     */
    suspend fun getFileSize(hash: String): Long {
        return dao.getByHash(hash)?.fileSize ?: 0
    }
}
