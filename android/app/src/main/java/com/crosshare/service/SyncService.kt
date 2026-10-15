package com.crosshare.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.Binder
import android.os.IBinder
import android.util.Base64
import android.util.Log
import com.crosshare.MainActivity
import com.crosshare.R
import com.crosshare.data.db.MediaEntity
import com.crosshare.data.repository.LibraryClient
import com.crosshare.data.repository.MediaRepository
import java.net.HttpURLConnection
import java.net.URL
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.json.JSONObject

/** Foreground service that scans local media and pushes it to the paired computer. */
class SyncService : Service() {

    companion object {
        private const val TAG = "SyncService"
        private const val NOTIFICATION_ID = 1
        private const val CHANNEL_ID = "crosshare_sync"

        fun start(context: Context) {
            val intent = Intent(context, SyncService::class.java)
            context.startForegroundService(intent)
        }

        fun stop(context: Context) {
            val intent = Intent(context, SyncService::class.java)
            context.stopService(intent)
        }
    }

    // Service state
    data class ServiceState(
        val isRunning: Boolean = false,
        val totalMedia: Int = 0,
        val syncedMedia: Int = 0,
        val indexing: Boolean = false,
        val syncing: Boolean = false,
        val syncDone: Int = 0,
        val syncTotal: Int = 0,
        val currentFile: String? = null,
        val lastError: String? = null,
        val pendingUploads: Int = 0,
    )

    private val _state = MutableStateFlow(ServiceState())
    val state: StateFlow<ServiceState> = _state.asStateFlow()

    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())
    private lateinit var repository: MediaRepository
    private val transferMutex = Mutex()
    private val queueLock = Any()
    private var receiverJob: Job? = null
    private val queuePrefs by lazy {
        getSharedPreferences("crosshare_transfers", Context.MODE_PRIVATE)
    }

    fun uploadSelected(hashes: Set<String>) =
        synchronized(queueLock) {
            val pending = queuePrefs.getStringSet("uploads", emptySet()).orEmpty().toMutableSet()
            pending.addAll(hashes)
            queuePrefs.edit().putStringSet("uploads", pending).commit()
            _state.value = _state.value.copy(pendingUploads = pending.size)
            scope.launch { transferMutex.withLock { syncToComputer(pending) } }
        }

    private fun startReceiver() {
        if (receiverJob?.isActive == true) return
        receiverJob =
            scope.launch {
                val client = LibraryClient(applicationContext)
                while (isActive) {
                    transferMutex.withLock {
                        val uploads =
                            queuePrefs.getStringSet("uploads", emptySet()).orEmpty().toSet()
                        _state.value = _state.value.copy(pendingUploads = uploads.size)
                        if (uploads.isNotEmpty()) syncToComputer(uploads)
                        if (client.paired) {
                            try {
                                val pending =
                                    client.deliveries(pending = true).filter {
                                        it.status == "pending"
                                    }
                                for ((index, task) in pending.withIndex()) {
                                    _state.value =
                                        _state.value.copy(
                                            syncing = true,
                                            currentFile = "接收 ${task.name}",
                                            syncDone = index,
                                            syncTotal = pending.size,
                                            lastError = null,
                                        )
                                    updateNotification("正在接收 ${task.name}")
                                    try {
                                        client.receive(task)
                                    } catch (e: CancellationException) {
                                        throw e
                                    } catch (e: Exception) {
                                        runCatching {
                                            client.request(
                                                "/transfers/${task.id}/ack",
                                                JSONObject().put("error", e.message ?: "接收失败"),
                                            )
                                        }
                                        _state.value =
                                            _state.value.copy(
                                                lastError = "接收失败：${task.name}：${e.message}"
                                            )
                                        continue
                                    }
                                    _state.value = _state.value.copy(syncDone = index + 1)
                                }
                                if (pending.isNotEmpty()) {
                                    indexMedia()
                                    updateNotification("接收完成 · 已保存到系统相册")
                                }
                                _state.value =
                                    _state.value.copy(syncing = false, currentFile = null)
                            } catch (e: CancellationException) {
                                throw e
                            } catch (e: Exception) {
                                _state.value =
                                    _state.value.copy(
                                        syncing = false,
                                        currentFile = null,
                                        lastError = "接收失败：${e.message}",
                                    )
                            }
                        }
                    }
                    delay(15000)
                }
            }
    }

    // Binder for activity connection
    inner class LocalBinder : Binder() {
        fun getService(): SyncService = this@SyncService
    }

    private val binder = LocalBinder()

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onCreate() {
        super.onCreate()
        Log.i(TAG, "SyncService created")

        repository = MediaRepository(applicationContext)
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Log.i(TAG, "SyncService starting")

        startForeground(
            NOTIFICATION_ID,
            createNotification(getString(R.string.sync_notification_text)),
        )
        startReceiver()

        if (intent?.action == "com.crosshare.ACTION_RESCAN") {
            rescan()
            return START_STICKY
        }
        if (intent?.action == "com.crosshare.ACTION_SYNC_NOW") {
            syncNow()
            return START_STICKY
        }

        // Scan and index media in background
        rescan()

        _state.value = _state.value.copy(isRunning = true)

        return START_STICKY
    }

    override fun onDestroy() {
        Log.i(TAG, "SyncService stopping")

        scope.cancel()

        _state.value = ServiceState(isRunning = false)

        super.onDestroy()
    }

    private suspend fun indexMedia() {
        _state.value = _state.value.copy(indexing = true)
        updateNotification("正在扫描媒体文件...")

        try {
            val newCount = repository.scanAndIndex()
            val total = repository.getTotalCount()
            val synced = repository.getSyncedCount()

            _state.value =
                _state.value.copy(totalMedia = total, syncedMedia = synced, indexing = false)

            updateNotification("就绪 · $total 个文件 · 可手动同步")
            Log.i(TAG, "索引完成: 新增 $newCount, 总计 $total, 已同步 $synced")
        } catch (e: Exception) {
            Log.e(TAG, "索引失败: ${e.message}", e)
            _state.value = _state.value.copy(indexing = false, lastError = "索引失败: ${e.message}")
            updateNotification("索引出错")
        }
    }

    /** Trigger a re-scan of media files. */
    fun rescan() {
        scope.launch { transferMutex.withLock { indexMedia() } }
    }

    /** Push unsynced media to the paired computer. */
    fun syncNow() {
        scope.launch {
            transferMutex.withLock {
                repository.scanAndIndex()
                uploadSelected(repository.getUnsyncedItems().map { it.contentHash }.toSet())
            }
        }
    }

    private suspend fun syncToComputer(selected: Set<String>) {
        val prefs =
            applicationContext.getSharedPreferences("crosshare_server", Context.MODE_PRIVATE)
        val token = prefs.getString("paired_token", null)
        val host = prefs.getString("paired_computer_host", null)
        val port = prefs.getInt("paired_computer_port", 8384)

        if (token.isNullOrBlank() || host.isNullOrBlank()) {
            _state.value = _state.value.copy(lastError = "还没有配对电脑")
            updateNotification("还没有配对电脑")
            return
        }

        _state.value =
            _state.value.copy(
                syncing = true,
                syncDone = 0,
                syncTotal = 0,
                currentFile = null,
                lastError = null,
            )
        updateNotification("正在准备同步...")

        try {
            repository.scanAndIndex()
            val desktopHashes = fetchSyncedHashes(host, port, token)
            val unsynced = selected.mapNotNull { repository.getByHash(it) }
            val missing = selected - unsynced.map { it.contentHash }.toSet()
            if (missing.isNotEmpty()) {
                clearQueued(missing)
                error("部分待上传照片已移除或不在授权范围内，请重新选择")
            }

            val alreadyOnDesktop =
                unsynced.filter { desktopHashes.contains(it.contentHash) }.map { it.contentHash }
            if (alreadyOnDesktop.isNotEmpty()) {
                repository.markSyncedBatch(alreadyOnDesktop)
                clearQueued(alreadyOnDesktop.toSet())
            }

            val toUpload = unsynced.filterNot { desktopHashes.contains(it.contentHash) }
            _state.value = _state.value.copy(syncTotal = toUpload.size, syncDone = 0)

            for ((index, item) in toUpload.withIndex()) {
                _state.value =
                    _state.value.copy(
                        currentFile = item.fileName,
                        syncDone = index,
                        syncTotal = toUpload.size,
                    )
                updateNotification("正在上传 ${index + 1}/${toUpload.size}: ${item.fileName}")

                uploadMedia(host, port, token, item)
                repository.markSynced(item.contentHash)
                clearQueued(setOf(item.contentHash))
            }

            val total = repository.getTotalCount()
            val synced = repository.getSyncedCount()
            _state.value =
                _state.value.copy(
                    isRunning = true,
                    totalMedia = total,
                    syncedMedia = synced,
                    syncing = false,
                    syncDone = toUpload.size,
                    syncTotal = toUpload.size,
                    currentFile = null,
                )
            updateNotification("同步完成 · 已同步 $synced/$total")
        } catch (e: Exception) {
            Log.e(TAG, "同步失败: ${e.message}", e)
            _state.value =
                _state.value.copy(
                    syncing = false,
                    currentFile = null,
                    lastError = "同步失败: ${e.message}",
                )
            updateNotification("同步失败")
        }
    }

    private fun fetchSyncedHashes(host: String, port: Int, token: String): Set<String> {
        val conn =
            (URL("http://$host:$port/api/synced").openConnection() as HttpURLConnection).apply {
                requestMethod = "GET"
                setRequestProperty("Authorization", "Bearer $token")
                connectTimeout = 10000
                readTimeout = 30000
            }

        return try {
            if (conn.responseCode != 200) {
                throw IllegalStateException(
                    conn.errorStream?.bufferedReader()?.readText() ?: "电脑返回 ${conn.responseCode}"
                )
            }

            val response = JSONObject(conn.inputStream.bufferedReader().readText())
            val hashes = response.optJSONArray("hashes")
            buildSet {
                if (hashes != null) {
                    for (i in 0 until hashes.length()) {
                        add(hashes.getString(i))
                    }
                }
            }
        } finally {
            conn.disconnect()
        }
    }

    private suspend fun uploadMedia(host: String, port: Int, token: String, item: MediaEntity) {
        val input =
            repository.openInputStream(item.contentHash)
                ?: throw IllegalStateException("无法读取 ${item.fileName}")

        val conn =
            (URL("http://$host:$port/api/media/upload/${item.contentHash}").openConnection()
                    as HttpURLConnection)
                .apply {
                    requestMethod = "POST"
                    doOutput = true
                    setRequestProperty("Authorization", "Bearer $token")
                    setRequestProperty(
                        "Content-Type",
                        item.mimeType.ifBlank { "application/octet-stream" },
                    )
                    setRequestProperty(
                        "X-CrossHare-File-Name-Base64",
                        Base64.encodeToString(
                            item.fileName.toByteArray(Charsets.UTF_8),
                            Base64.NO_WRAP,
                        ),
                    )
                    setRequestProperty("X-CrossHare-File-Size", item.fileSize.toString())
                    setRequestProperty("X-CrossHare-Media-Type", item.mediaType)
                    setRequestProperty("X-CrossHare-Date-Taken-Ms", item.dateTaken.toString())
                    setFixedLengthStreamingMode(item.fileSize)
                    connectTimeout = 10000
                    readTimeout = 60000
                }

        try {
            input.use { source ->
                conn.outputStream.use { target -> source.copyTo(target, bufferSize = 256 * 1024) }
            }

            if (conn.responseCode != 200) {
                throw IllegalStateException(
                    conn.errorStream?.bufferedReader()?.readText() ?: "电脑返回 ${conn.responseCode}"
                )
            }
        } finally {
            conn.disconnect()
        }
    }

    private fun clearQueued(done: Set<String>) =
        synchronized(queueLock) {
            val remaining = queuePrefs.getStringSet("uploads", emptySet()).orEmpty().toMutableSet()
            remaining.removeAll(done)
            queuePrefs.edit().putStringSet("uploads", remaining).commit()
            _state.value = _state.value.copy(pendingUploads = remaining.size)
        }

    override fun onTimeout(startId: Int, fgsType: Int) {
        stopSelf()
    }

    // Notification helpers

    private fun createNotificationChannel() {
        val channel =
            NotificationChannel(
                    CHANNEL_ID,
                    getString(R.string.sync_notification_channel),
                    NotificationManager.IMPORTANCE_LOW,
                )
                .apply {
                    description = "CrossHare 同步服务"
                    setShowBadge(false)
                }

        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(channel)
    }

    private fun createNotification(text: String): Notification {
        val pendingIntent =
            PendingIntent.getActivity(
                this,
                0,
                Intent(this, MainActivity::class.java),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
            )

        return Notification.Builder(this, CHANNEL_ID)
            .setContentTitle(getString(R.string.sync_notification_title))
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_menu_share)
            .setContentIntent(pendingIntent)
            .setOngoing(true)
            .build()
    }

    private fun updateNotification(text: String) {
        val manager = getSystemService(NotificationManager::class.java)
        manager.notify(NOTIFICATION_ID, createNotification(text))
    }
}
