package com.crosshare.data.db

import androidx.room.*
import kotlinx.coroutines.flow.Flow

data class FolderSummary(val path: String, val count: Int)

@Dao
interface MediaDao {

    @Query(
        "SELECT * FROM media_items WHERE (:folder IS NULL OR substr(filePath,1,length(filePath)-length(fileName)) = :folder) ORDER BY dateTaken DESC,id DESC LIMIT :limit OFFSET :offset"
    )
    suspend fun page(limit: Int, offset: Int, folder: String?): List<MediaEntity>

    @Query("SELECT * FROM media_items WHERE contentHash IN (:hashes)")
    suspend fun matches(hashes: List<String>): List<MediaEntity>

    @Query(
        "SELECT substr(filePath,1,length(filePath)-length(fileName)) AS path,COUNT(*) AS count FROM media_items GROUP BY path ORDER BY path"
    )
    fun folders(): Flow<List<FolderSummary>>

    @Query(
        "SELECT * FROM media_items WHERE synced=1 ORDER BY dateTaken DESC,id DESC LIMIT 48 OFFSET :offset"
    )
    fun uploadedPage(offset: Int): Flow<List<MediaEntity>>

    @Query("SELECT COUNT(*) FROM media_items WHERE synced=1") fun uploadedCount(): Flow<Int>

    @Query("SELECT * FROM media_items ORDER BY dateTaken DESC")
    fun getAllMedia(): Flow<List<MediaEntity>>

    @Query("SELECT * FROM media_items WHERE synced = 0 ORDER BY dateTaken DESC")
    fun getUnsyncedMedia(): Flow<List<MediaEntity>>

    @Query("SELECT * FROM media_items WHERE synced = 0 ORDER BY dateTaken DESC")
    suspend fun getUnsyncedItems(): List<MediaEntity>

    @Query("SELECT contentHash FROM media_items") suspend fun getAllHashes(): List<String>

    @Query("SELECT COUNT(*) FROM media_items") suspend fun getTotalCount(): Int

    @Query("SELECT COUNT(*) FROM media_items WHERE synced = 1") suspend fun getSyncedCount(): Int

    @Query("SELECT COALESCE(SUM(fileSize), 0) FROM media_items WHERE synced = 1")
    suspend fun getSyncedSize(): Long

    @Query("SELECT * FROM media_items WHERE contentHash = :hash LIMIT 1")
    suspend fun getByHash(hash: String): MediaEntity?

    @Query(
        "SELECT * FROM media_items WHERE mediaStoreId = :mediaStoreId AND mediaType = :mediaType LIMIT 1"
    )
    suspend fun getByMediaStoreId(mediaStoreId: Long, mediaType: String): MediaEntity?

    @Query("DELETE FROM media_items WHERE id = :id") suspend fun deleteById(id: Long)

    @Query("SELECT mediaStoreId FROM media_items WHERE mediaType = :type")
    suspend fun idsForType(type: String): List<Long>

    @Query("DELETE FROM media_items WHERE mediaType = :type AND mediaStoreId IN (:ids)")
    suspend fun deleteTypeIds(type: String, ids: List<Long>)

    @Query("DELETE FROM media_items WHERE mediaType = :type AND mediaStoreId NOT IN (:ids)")
    suspend fun pruneType(type: String, ids: List<Long>)

    @Insert(onConflict = OnConflictStrategy.IGNORE) suspend fun insert(item: MediaEntity): Long

    @Insert(onConflict = OnConflictStrategy.IGNORE) suspend fun insertAll(items: List<MediaEntity>)

    @Query("UPDATE media_items SET synced = 1 WHERE contentHash = :hash")
    suspend fun markSynced(hash: String)

    @Query("UPDATE media_items SET synced = 1 WHERE contentHash IN (:hashes)")
    suspend fun markSyncedBatch(hashes: List<String>)

    @Query("DELETE FROM media_items WHERE mediaStoreId NOT IN (:validIds)")
    suspend fun deleteRemovedMedia(validIds: List<Long>)

    @Query("SELECT contentHash FROM media_items WHERE synced = 0")
    suspend fun getUnsyncedHashes(): List<String>
}
