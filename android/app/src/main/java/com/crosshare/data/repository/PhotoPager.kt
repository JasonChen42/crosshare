package com.crosshare.data.repository

import com.crosshare.data.db.MediaEntity
import java.time.Instant
import kotlinx.coroutines.CancellationException

/** Only the current page's media objects are retained; history stores cursors and hashes. */
data class PagedPhoto(
    val hash: String,
    val name: String,
    val local: MediaEntity?,
    val remote: LibraryClient.Asset?,
)

data class PhotoCursor(val local: Int = 0, val remote: Int = 0)

data class PhotoPage(
    val photos: List<PagedPhoto>,
    val next: PhotoCursor,
    val hasNext: Boolean,
    val remoteError: String? = null,
)

class PhotoPager(
    private val localPage: suspend (Int, String?) -> List<MediaEntity>,
    private val remotePage: suspend (Int, Long) -> List<LibraryClient.Asset>,
    private val localMatches: suspend (List<String>) -> List<MediaEntity>,
) {
    companion object {
        const val SIZE = 48
    }

    suspend fun load(
        cursor: PhotoCursor,
        source: Int,
        folder: String?,
        album: Long,
        paired: Boolean,
        previousHashes: Set<String>,
    ): PhotoPage {
        var li = cursor.local
        var ri = cursor.remote
        var local = emptyList<MediaEntity>()
        var remote = emptyList<LibraryClient.Asset>()
        var lp = 0
        var rp = 0
        var localEnd = source == 2 || album != 0L
        var remoteEnd = source == 1 || folder != null || !paired
        val seen = previousHashes.toMutableSet()
        var remoteError: String? = null
        suspend fun peekLocal(): MediaEntity? {
            if (lp == local.size && !localEnd) {
                local = localPage(li, folder)
                lp = 0
                localEnd = local.size < SIZE
            }
            return local.getOrNull(lp)
        }
        suspend fun peekRemote(): LibraryClient.Asset? {
            if (rp == remote.size && !remoteEnd) {
                remote =
                    try {
                        remotePage(ri, album)
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: Exception) {
                        remoteError = "电脑暂时不可用，已显示本机照片：${e.message}"
                        emptyList()
                    }
                rp = 0
                remoteEnd = remote.size < SIZE
            }
            return remote.getOrNull(rp)
        }
        val out = mutableListOf<PagedPhoto>()
        while (true) {
            var l = peekLocal()
            var r = peekRemote()
            if (l != null && l.contentHash in seen) {
                li++
                lp++
                continue
            }
            if (r != null && r.hash in seen) {
                ri++
                rp++
                continue
            }
            if (l == null && r == null) break
            if (out.size == SIZE) break
            val remoteDate =
                r?.let { runCatching { Instant.parse(it.date).toEpochMilli() }.getOrDefault(0L) }
                    ?: Long.MIN_VALUE
            val photo =
                when {
                    l != null && r != null && l.contentHash == r.hash -> {
                        li++
                        lp++
                        ri++
                        rp++
                        PagedPhoto(l.contentHash, l.fileName, l, r)
                    }
                    l != null && (r == null || l.dateTaken >= remoteDate) -> {
                        li++
                        lp++
                        PagedPhoto(l.contentHash, l.fileName, l, null)
                    }
                    else -> {
                        ri++
                        rp++
                        PagedPhoto(r!!.hash, r.name, null, r)
                    }
                }
            seen.add(photo.hash)
            out.add(photo)
        }
        val matches =
            localMatches(out.filter { it.local == null }.map { it.hash }).associateBy {
                it.contentHash
            }
        return PhotoPage(
            out.map { if (it.local == null) it.copy(local = matches[it.hash]) else it },
            PhotoCursor(li, ri),
            peekLocal() != null || peekRemote() != null,
            remoteError,
        )
    }
}
