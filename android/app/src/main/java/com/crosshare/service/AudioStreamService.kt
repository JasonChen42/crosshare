package com.crosshare.service

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import android.os.Binder
import android.os.Build
import android.os.IBinder
import android.os.Process
import android.util.Log
import androidx.core.content.ContextCompat
import com.crosshare.MainActivity
import com.crosshare.R
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import org.json.JSONObject
import java.io.BufferedOutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.util.ArrayDeque
import java.util.Locale
import java.util.UUID
import kotlin.math.max
import kotlin.math.sqrt

class AudioStreamService : Service() {

    companion object {
        private const val TAG = "AudioStreamService"
        private const val ACTION_START = "com.crosshare.audio.START"
        private const val ACTION_STOP = "com.crosshare.audio.STOP"
        private const val NOTIFICATION_ID = 2
        private const val CHANNEL_ID = "crosshare_audio"
        const val PREFS_NAME = "crosshare_server"
        const val PREF_AUDIO_SENSITIVITY = "audio_sensitivity"
        const val AUDIO_SENSITIVITY_STRICT = "strict"
        const val AUDIO_SENSITIVITY_BALANCED = "balanced"
        const val AUDIO_SENSITIVITY_SENSITIVE = "sensitive"

        private const val SAMPLE_RATE = 16000
        private const val CHANNEL_COUNT = 1
        private const val BITS_PER_SAMPLE = 16

        private const val FRAME_DURATION_MS = 20
        private const val FRAME_BYTES =
            SAMPLE_RATE * CHANNEL_COUNT * (BITS_PER_SAMPLE / 8) * FRAME_DURATION_MS / 1000
        private const val PRE_ROLL_FRAMES = 10
        private const val MAX_SEGMENT_FRAMES = 1000

        fun start(context: Context) {
            val intent = Intent(context, AudioStreamService::class.java).apply {
                action = ACTION_START
            }
            ContextCompat.startForegroundService(context, intent)
        }

        fun stop(context: Context) {
            val intent = Intent(context, AudioStreamService::class.java).apply {
                action = ACTION_STOP
            }
            context.startService(intent)
        }
    }

    data class StreamState(
        val isRunning: Boolean = false,
        val connected: Boolean = false,
        val computerName: String? = null,
        val bytesSent: Long = 0,
        val sampleRate: Int = SAMPLE_RATE,
        val channels: Int = CHANNEL_COUNT,
        val bitsPerSample: Int = BITS_PER_SAMPLE,
        val startedAtMs: Long? = null,
        val segmentsSent: Int = 0,
        val lastSavedFile: String? = null,
        val lastError: String? = null
    )

    private data class ListenerResult(
        val bytesSent: Long,
        val segmentsSent: Int,
        val lastSavedFile: String?
    )

    private data class SegmentUploadResult(
        val bytesSent: Long,
        val savedFile: String?
    )

    private data class DetectorConfig(
        val startNoiseMultiplier: Double,
        val startMinLevel: Double,
        val continueNoiseMultiplier: Double,
        val continueMinLevel: Double,
        val startSpeechFrames: Int,
        val stopSilenceFrames: Int
    )

    inner class LocalBinder : Binder() {
        fun getService(): AudioStreamService = this@AudioStreamService
    }

    private val binder = LocalBinder()
    private val scope = CoroutineScope(Dispatchers.IO + SupervisorJob())
    private val _state = MutableStateFlow(StreamState())
    val state: StateFlow<StreamState> = _state.asStateFlow()

    @Volatile
    private var stopRequested = false

    private var streamingJobActive = false
    private var audioRecord: AudioRecord? = null

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onCreate() {
        super.onCreate()
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                requestStop()
                return START_NOT_STICKY
            }
        }

        if (streamingJobActive) {
            return START_STICKY
        }

        startForegroundCompat(createNotification(getString(R.string.audio_notification_text)))
        startListening()
        return START_STICKY
    }

    override fun onDestroy() {
        stopRequested = true
        releaseRecorder()
        super.onDestroy()
    }

    private fun startListening() {
        val prefs = applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
        val token = prefs.getString("paired_token", null)
        val host = prefs.getString("paired_computer_host", null)
        val port = prefs.getInt("paired_computer_port", 8384)
        val computerName = prefs.getString("paired_computer_name", null)
        val detectorConfig = detectorConfigFor(
            prefs.getString(PREF_AUDIO_SENSITIVITY, AUDIO_SENSITIVITY_BALANCED)
        )

        if (token.isNullOrBlank() || host.isNullOrBlank()) {
            _state.value = _state.value.copy(
                isRunning = false,
                connected = false,
                computerName = computerName,
                lastError = "还没有配对电脑"
            )
            updateNotification("还没有配对电脑")
            stopSelf()
            return
        }

        stopRequested = false
        streamingJobActive = true
        _state.value = StreamState(
            isRunning = true,
            connected = false,
            computerName = computerName,
            startedAtMs = System.currentTimeMillis(),
            lastError = null
        )
        updateNotification("省电监听中，等待你说话")

        scope.launch {
            var result = ListenerResult(0, 0, null)
            var failure: Throwable? = null

            try {
                result = runListenerLoop(host, port, token, computerName, detectorConfig)
            } catch (t: Throwable) {
                if (!stopRequested) {
                    failure = t
                    Log.e(TAG, "Audio listener failed: ${t.message}", t)
                }
            } finally {
                releaseRecorder()
                streamingJobActive = false

                if (failure != null) {
                    _state.value = _state.value.copy(
                        isRunning = false,
                        connected = false,
                        lastError = "监听失败: ${failure.message}"
                    )
                    updateNotification("麦克风监听失败")
                } else {
                    _state.value = _state.value.copy(
                        isRunning = false,
                        connected = false,
                        bytesSent = result.bytesSent,
                        segmentsSent = result.segmentsSent,
                        lastSavedFile = result.lastSavedFile ?: _state.value.lastSavedFile,
                        lastError = null
                    )
                    updateNotification(
                        if (result.segmentsSent > 0) {
                            "监听已停止，已发送 ${result.segmentsSent} 段语音"
                        } else {
                            "麦克风监听已停止"
                        }
                    )
                }

                stopSelf()
            }
        }
    }

    private fun runListenerLoop(
        host: String,
        port: Int,
        token: String,
        computerName: String?,
        detectorConfig: DetectorConfig
    ): ListenerResult {
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
            throw IllegalStateException("缺少麦克风权限")
        }

        Process.setThreadPriority(Process.THREAD_PRIORITY_AUDIO)
        val displayName = computerName ?: "电脑"

        val minBufferSize = AudioRecord.getMinBufferSize(
            SAMPLE_RATE,
            AudioFormat.CHANNEL_IN_MONO,
            AudioFormat.ENCODING_PCM_16BIT
        )
        if (minBufferSize <= 0) {
            throw IllegalStateException("设备不支持 16kHz PCM 录音")
        }

        val recorder = AudioRecord.Builder()
            .setAudioSource(MediaRecorder.AudioSource.VOICE_RECOGNITION)
            .setAudioFormat(
                AudioFormat.Builder()
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .setSampleRate(SAMPLE_RATE)
                    .setChannelMask(AudioFormat.CHANNEL_IN_MONO)
                    .build()
            )
            .setBufferSizeInBytes(max(minBufferSize * 2, FRAME_BYTES * 8))
            .build()

        if (recorder.state != AudioRecord.STATE_INITIALIZED) {
            recorder.release()
            throw IllegalStateException("麦克风初始化失败")
        }
        audioRecord = recorder

        recorder.startRecording()
        if (recorder.recordingState != AudioRecord.RECORDSTATE_RECORDING) {
            throw IllegalStateException("无法开始录音")
        }

        val frameBuffer = ByteArray(FRAME_BYTES)
        val preRollFrames = ArrayDeque<ByteArray>()
        val speechLeadInFrames = ArrayDeque<ByteArray>()

        var uploader: SegmentUploader? = null
        var totalBytesSent = 0L
        var segmentsSent = 0
        var lastSavedFile: String? = null
        var consecutiveSpeechFrames = 0
        var trailingSilenceFrames = 0
        var segmentFrames = 0
        var noiseFloor = 140.0
        var lastIdleNotificationAt = 0L

        try {
            while (!stopRequested && scope.isActive) {
                val read = recorder.read(frameBuffer, 0, frameBuffer.size)
                if (read <= 0) {
                    if (stopRequested) {
                        break
                    }
                    throw IllegalStateException("麦克风读取失败 ($read)")
                }

                val frame = frameBuffer.copyOf(read)
                val level = calculateFrameLevel(frame)

                if (uploader == null) {
                    val threshold = max(noiseFloor * detectorConfig.startNoiseMultiplier, detectorConfig.startMinLevel)
                    val isSpeech = level >= threshold
                    if (isSpeech) {
                        speechLeadInFrames.addLast(frame)
                        consecutiveSpeechFrames += 1

                        if (consecutiveSpeechFrames >= detectorConfig.startSpeechFrames) {
                            val nextUploader = SegmentUploader(host, port, token)
                            uploader = nextUploader

                            for (bufferedFrame in preRollFrames) {
                                totalBytesSent += nextUploader.write(bufferedFrame)
                            }
                            for (bufferedFrame in speechLeadInFrames) {
                                totalBytesSent += nextUploader.write(bufferedFrame)
                            }

                            segmentFrames = preRollFrames.size + speechLeadInFrames.size
                            trailingSilenceFrames = 0
                            consecutiveSpeechFrames = 0
                            preRollFrames.clear()
                            speechLeadInFrames.clear()

                            _state.value = _state.value.copy(
                                isRunning = true,
                                connected = true,
                                computerName = computerName,
                                bytesSent = totalBytesSent,
                                segmentsSent = segmentsSent,
                                lastError = null
                            )
                            updateNotification("检测到说话，正在发送到 $displayName")
                        }
                    } else {
                        pushFrame(preRollFrames, frame, PRE_ROLL_FRAMES)
                        speechLeadInFrames.clear()
                        consecutiveSpeechFrames = 0
                        noiseFloor = updateNoiseFloor(noiseFloor, level)

                        val now = System.currentTimeMillis()
                        if (now - lastIdleNotificationAt >= 8000) {
                            updateNotification("省电监听中，等待你说话")
                            lastIdleNotificationAt = now
                        }
                    }
                } else {
                    val threshold = max(noiseFloor * detectorConfig.continueNoiseMultiplier, detectorConfig.continueMinLevel)
                    val isSpeech = level >= threshold
                    totalBytesSent += uploader.write(frame)
                    segmentFrames += 1

                    if (isSpeech) {
                        trailingSilenceFrames = 0
                    } else {
                        trailingSilenceFrames += 1
                        noiseFloor = updateNoiseFloor(noiseFloor, level)
                    }

                    _state.value = _state.value.copy(
                        isRunning = true,
                        connected = true,
                        computerName = computerName,
                        bytesSent = totalBytesSent,
                        segmentsSent = segmentsSent,
                        lastError = null
                    )

                    if (trailingSilenceFrames >= detectorConfig.stopSilenceFrames || segmentFrames >= MAX_SEGMENT_FRAMES) {
                        val uploadResult = uploader.finish()
                        uploader = null
                        segmentsSent += 1
                        lastSavedFile = uploadResult.savedFile ?: lastSavedFile
                        trailingSilenceFrames = 0
                        segmentFrames = 0
                        speechLeadInFrames.clear()
                        consecutiveSpeechFrames = 0

                        _state.value = _state.value.copy(
                            isRunning = true,
                            connected = false,
                            computerName = computerName,
                            bytesSent = totalBytesSent,
                            segmentsSent = segmentsSent,
                            lastSavedFile = lastSavedFile,
                            lastError = null
                        )
                        updateNotification("省电监听中，已发送 ${segmentsSent} 段语音")
                    }
                }
            }

            if (uploader != null && uploader.bytesSent > 0) {
                val uploadResult = uploader.finish()
                segmentsSent += 1
                lastSavedFile = uploadResult.savedFile ?: lastSavedFile
            } else {
                uploader?.abort()
            }
        } catch (t: Throwable) {
            uploader?.abort()
            throw t
        }

        return ListenerResult(
            bytesSent = totalBytesSent,
            segmentsSent = segmentsSent,
            lastSavedFile = lastSavedFile
        )
    }

    private fun requestStop() {
        stopRequested = true
        try {
            audioRecord?.stop()
        } catch (_: Throwable) {
        }
    }

    private fun releaseRecorder() {
        try {
            audioRecord?.stop()
        } catch (_: Throwable) {
        }
        try {
            audioRecord?.release()
        } catch (_: Throwable) {
        }
        audioRecord = null
    }

    private fun createNotificationChannel() {
        val manager = getSystemService(NotificationManager::class.java)
        val channel = NotificationChannel(
            CHANNEL_ID,
            getString(R.string.audio_notification_channel),
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = "CrossHare 低功耗麦克风监听"
        }
        manager.createNotificationChannel(channel)
    }

    private fun createNotification(text: String): Notification {
        val launchIntent = Intent(this, MainActivity::class.java)
        val pendingIntent = PendingIntent.getActivity(
            this,
            0,
            launchIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        return Notification.Builder(this, CHANNEL_ID)
            .setContentTitle(getString(R.string.audio_notification_title))
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_btn_speak_now)
            .setContentIntent(pendingIntent)
            .setOngoing(true)
            .build()
    }

    private fun startForegroundCompat(notification: Notification) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            startForeground(
                NOTIFICATION_ID,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE
            )
        } else {
            startForeground(NOTIFICATION_ID, notification)
        }
    }

    private fun updateNotification(text: String) {
        val manager = getSystemService(NotificationManager::class.java)
        manager.notify(NOTIFICATION_ID, createNotification(text))
    }

    private fun calculateFrameLevel(frame: ByteArray): Double {
        if (frame.isEmpty()) {
            return 0.0
        }

        var sumSquares = 0.0
        var sampleCount = 0
        var index = 0
        while (index + 1 < frame.size) {
            val sample = ((frame[index + 1].toInt() shl 8) or (frame[index].toInt() and 0xff)).toShort().toInt()
            sumSquares += sample * sample.toDouble()
            sampleCount += 1
            index += 2
        }

        if (sampleCount == 0) {
            return 0.0
        }
        return sqrt(sumSquares / sampleCount)
    }

    private fun updateNoiseFloor(currentFloor: Double, frameLevel: Double): Double {
        val clampedLevel = frameLevel.coerceAtLeast(60.0)
        return currentFloor * 0.97 + clampedLevel * 0.03
    }

    private fun pushFrame(buffer: ArrayDeque<ByteArray>, frame: ByteArray, maxFrames: Int) {
        if (buffer.size >= maxFrames) {
            buffer.removeFirst()
        }
        buffer.addLast(frame)
    }

    private fun detectorConfigFor(key: String?): DetectorConfig {
        return when (key) {
            AUDIO_SENSITIVITY_SENSITIVE -> DetectorConfig(
                startNoiseMultiplier = 2.7,
                startMinLevel = 380.0,
                continueNoiseMultiplier = 2.0,
                continueMinLevel = 260.0,
                startSpeechFrames = 3,
                stopSilenceFrames = 60
            )

            AUDIO_SENSITIVITY_STRICT -> DetectorConfig(
                startNoiseMultiplier = 4.5,
                startMinLevel = 950.0,
                continueNoiseMultiplier = 3.2,
                continueMinLevel = 700.0,
                startSpeechFrames = 6,
                stopSilenceFrames = 70
            )

            else -> DetectorConfig(
                startNoiseMultiplier = 3.4,
                startMinLevel = 650.0,
                continueNoiseMultiplier = 2.5,
                continueMinLevel = 460.0,
                startSpeechFrames = 5,
                stopSilenceFrames = 65
            )
        }
    }

    private fun formatBytes(bytes: Long): String {
        if (bytes < 1024) return "$bytes B"
        val kb = bytes / 1024.0
        if (kb < 1024) return String.format(Locale.US, "%.1f KB", kb)
        val mb = kb / 1024.0
        return String.format(Locale.US, "%.1f MB", mb)
    }

    private inner class SegmentUploader(
        host: String,
        port: Int,
        token: String
    ) {
        private val connection: HttpURLConnection =
            (URL("http://$host:$port/api/audio/stream").openConnection() as HttpURLConnection).apply {
                requestMethod = "POST"
                doOutput = true
                connectTimeout = 10000
                readTimeout = 30000
                setChunkedStreamingMode(16 * 1024)
                setRequestProperty("Authorization", "Bearer $token")
                setRequestProperty("Content-Type", "application/octet-stream")
                setRequestProperty("X-CrossHare-Audio-Session-ID", UUID.randomUUID().toString())
                setRequestProperty("X-CrossHare-Audio-Sample-Rate", SAMPLE_RATE.toString())
                setRequestProperty("X-CrossHare-Audio-Channels", CHANNEL_COUNT.toString())
                setRequestProperty("X-CrossHare-Audio-Bits-Per-Sample", BITS_PER_SAMPLE.toString())
            }

        private val output = BufferedOutputStream(connection.outputStream, 16 * 1024)
        var bytesSent: Long = 0
            private set

        fun write(frame: ByteArray): Long {
            output.write(frame)
            bytesSent += frame.size.toLong()
            return frame.size.toLong()
        }

        fun finish(): SegmentUploadResult {
            output.flush()
            output.close()

            val responseCode = connection.responseCode
            val responseText = try {
                if (responseCode in 200..299) {
                    connection.inputStream.bufferedReader().use { it.readText() }
                } else {
                    connection.errorStream?.bufferedReader()?.use { it.readText() } ?: ""
                }
            } finally {
                connection.disconnect()
            }

            if (responseCode !in 200..299) {
                throw IllegalStateException(responseText.ifBlank { "电脑返回 $responseCode" })
            }

            val savedFile = try {
                JSONObject(responseText).optString("file").ifBlank { null }
            } catch (_: Throwable) {
                null
            }

            Log.i(TAG, "Uploaded segment ${formatBytes(bytesSent)} saved as ${savedFile ?: "unknown"}")
            return SegmentUploadResult(bytesSent = bytesSent, savedFile = savedFile)
        }

        fun abort() {
            try {
                output.close()
            } catch (_: Throwable) {
            }
            connection.disconnect()
        }
    }
}
