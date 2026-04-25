package com.crosshare.data.db

import androidx.room.Entity
import androidx.room.Index
import androidx.room.PrimaryKey

/**
 * Represents a media file (photo or video) tracked by CrossHare.
 * The content_hash is computed as SHA-256 of the file bytes.
 */
@Entity(
    tableName = "media_items",
    indices = [
        Index(value = ["contentHash"], unique = true),
        Index(value = ["mediaStoreId"], unique = true)
    ]
)
data class MediaEntity(
    @PrimaryKey(autoGenerate = true)
    val id: Long = 0,

    /** MediaStore ID for this file */
    val mediaStoreId: Long,

    /** SHA-256 content hash (hex string) */
    val contentHash: String,

    /** Original filename */
    val fileName: String,

    /** Absolute file path or content URI */
    val filePath: String,

    /** File size in bytes */
    val fileSize: Long,

    /** MIME type (image/jpeg, video/mp4, etc.) */
    val mimeType: String,

    /** "photo" or "video" */
    val mediaType: String,

    /** Date the photo/video was taken (epoch millis) */
    val dateTaken: Long,

    /** Whether this file has been synced to a computer */
    val synced: Boolean = false,

    /** When the hash was computed (epoch millis) */
    val indexedAt: Long = System.currentTimeMillis()
)
