package com.crosshare.data.db

import androidx.room.*
import kotlinx.coroutines.flow.Flow

@Dao
interface MediaDao {

    @Query("SELECT * FROM media_items ORDER BY dateTaken DESC")
    fun getAllMedia(): Flow<List<MediaEntity>>

    @Query("SELECT * FROM media_items WHERE synced = 0 ORDER BY dateTaken DESC")
    fun getUnsyncedMedia(): Flow<List<MediaEntity>>

    @Query("SELECT * FROM media_items WHERE synced = 0 ORDER BY dateTaken DESC")
    suspend fun getUnsyncedItems(): List<MediaEntity>

    @Query("SELECT contentHash FROM media_items")
    suspend fun getAllHashes(): List<String>

    @Query("SELECT COUNT(*) FROM media_items")
    suspend fun getTotalCount(): Int

    @Query("SELECT COUNT(*) FROM media_items WHERE synced = 1")
    suspend fun getSyncedCount(): Int

    @Query("SELECT COALESCE(SUM(fileSize), 0) FROM media_items WHERE synced = 1")
    suspend fun getSyncedSize(): Long

    @Query("SELECT * FROM media_items WHERE contentHash = :hash LIMIT 1")
    suspend fun getByHash(hash: String): MediaEntity?

    @Query("SELECT * FROM media_items WHERE mediaStoreId = :mediaStoreId LIMIT 1")
    suspend fun getByMediaStoreId(mediaStoreId: Long): MediaEntity?

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insert(item: MediaEntity): Long

    @Insert(onConflict = OnConflictStrategy.IGNORE)
    suspend fun insertAll(items: List<MediaEntity>)

    @Query("UPDATE media_items SET synced = 1 WHERE contentHash = :hash")
    suspend fun markSynced(hash: String)

    @Query("UPDATE media_items SET synced = 1 WHERE contentHash IN (:hashes)")
    suspend fun markSyncedBatch(hashes: List<String>)

    @Query("DELETE FROM media_items WHERE mediaStoreId NOT IN (:validIds)")
    suspend fun deleteRemovedMedia(validIds: List<Long>)

    @Query("SELECT contentHash FROM media_items WHERE synced = 0")
    suspend fun getUnsyncedHashes(): List<String>
}
