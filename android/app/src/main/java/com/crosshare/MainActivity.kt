package com.crosshare

import android.Manifest
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.os.IBinder
import android.util.Log
import android.widget.Toast
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.runtime.*
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.crosshare.service.AudioStreamService
import com.crosshare.service.SyncService
import com.crosshare.service.shouldAutoStartSync
import com.crosshare.ui.screens.PhotoHomeScreen
import com.crosshare.ui.screens.PairScreen
import com.crosshare.ui.screens.SettingsScreen
import com.crosshare.ui.theme.CrossHareTheme
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

class MainActivity : ComponentActivity() {

    companion object {
        private const val TAG = "MainActivity"
        private const val KEY_SYNC_STOPPED = "sync_stopped"
    }

    private data class PairingResult(
        val token: String,
        val computerName: String
    )

    private var syncService: SyncService? = null
    private var syncBound = mutableStateOf(false)
    private var audioService: AudioStreamService? = null
    private var audioBound = mutableStateOf(false)

    private val syncServiceConnection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
            syncService = (binder as SyncService.LocalBinder).getService()
            syncBound.value = true
            Log.i(TAG, "Bound to SyncService")
        }

        override fun onServiceDisconnected(name: ComponentName?) {
            syncService = null
            syncBound.value = false
        }
    }

    private val audioServiceConnection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
            audioService = (binder as AudioStreamService.LocalBinder).getService()
            audioBound.value = true
            Log.i(TAG, "Bound to AudioStreamService")
        }

        override fun onServiceDisconnected(name: ComponentName?) {
            audioService = null
            audioBound.value = false
        }
    }

    private val mediaPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions()
    ) { permissions ->
        val mediaGranted = if (Build.VERSION.SDK_INT >= 33) {
            checkSelfPermission(Manifest.permission.READ_MEDIA_IMAGES) == PackageManager.PERMISSION_GRANTED ||
                checkSelfPermission(Manifest.permission.READ_MEDIA_VIDEO) == PackageManager.PERMISSION_GRANTED ||
                (Build.VERSION.SDK_INT >= 34 && checkSelfPermission(Manifest.permission.READ_MEDIA_VISUAL_USER_SELECTED) == PackageManager.PERMISSION_GRANTED)
        } else checkSelfPermission(Manifest.permission.READ_EXTERNAL_STORAGE) == PackageManager.PERMISSION_GRANTED
        // Receiving app-owned images does not require broad gallery permission.
        startSyncService()
        if (!mediaGranted) {
            Toast.makeText(this, "需要媒体访问权限才能同步照片", Toast.LENGTH_LONG).show()
        }
    }

    private val audioPermissionLauncher = registerForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions()
    ) { permissions ->
        val allGranted = checkSelfPermission(Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED
        if (allGranted) {
            startAudioService()
        } else {
            Toast.makeText(this, "需要麦克风权限才能传输声音", Toast.LENGTH_LONG).show()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()

        setContent {
            CrossHareTheme {
                val navController = rememberNavController()
                val isSyncBound by syncBound
                val isAudioBound by audioBound
                val dummyState = remember { MutableStateFlow(SyncService.ServiceState()) }
                val activeFlow = if (isSyncBound && syncService != null) syncService!!.state else dummyState
                val serviceState by activeFlow.collectAsState()
                val dummyAudioState = remember { MutableStateFlow(AudioStreamService.StreamState()) }
                val activeAudioFlow = if (isAudioBound && audioService != null) audioService!!.state else dummyAudioState
                val audioState by activeAudioFlow.collectAsState()

                val prefs = remember {
                    getSharedPreferences("crosshare_server", Context.MODE_PRIVATE)
                }
                var pairedToken by remember {
                    mutableStateOf(prefs.getString("paired_token", null))
                }
                var pairedComputerName by remember {
                    mutableStateOf(prefs.getString("paired_computer_name", null))
                }
                var audioSensitivity by remember {
                    mutableStateOf(
                        prefs.getString(
                            AudioStreamService.PREF_AUDIO_SENSITIVITY,
                            AudioStreamService.AUDIO_SENSITIVITY_BALANCED
                        ) ?: AudioStreamService.AUDIO_SENSITIVITY_BALANCED
                    )
                }
                val isPaired = pairedToken != null
                val scope = rememberCoroutineScope()

                // A reinstall or process death leaves the sync service stopped,
                // which would grey out every upload button. Restart it here
                // unless the user paused transfers on purpose.
                LaunchedEffect(Unit) {
                    if (shouldAutoStartSync(isPaired, syncStoppedByUser())) {
                        checkPermissionsAndStart()
                    }
                }

                NavHost(navController = navController, startDestination = "home") {
                    composable("home") {
                        PhotoHomeScreen(
                            serviceState = serviceState,
                            audioState = audioState,
                            pairedComputerName = pairedComputerName,
                            onStartService = { checkPermissionsAndStart() },
                            onStopService = { stopSyncService() },
                            onStartAudio = { checkAudioPermissionAndStart() },
                            onStopAudio = { stopAudioService() },
                            onRescan = { syncService?.rescan() },
                            onUpload = { hashes ->
                                val service = syncService
                                if (service != null) service.uploadSelected(hashes)
                                else {
                                    // No bound service yet: persist the queue and
                                    // start the service, which drains it at once.
                                    SyncService.enqueue(this@MainActivity, hashes)
                                    checkPermissionsAndStart()
                                }
                            },
                            onNavigateToSettings = { navController.navigate("settings") }
                        )
                    }

                    composable("settings") {
                        SettingsScreen(
                            isPaired = isPaired,
                            pairedComputerName = pairedComputerName,
                            audioSensitivity = audioSensitivity,
                            onAudioSensitivityChange = { next ->
                                prefs.edit()
                                    .putString(AudioStreamService.PREF_AUDIO_SENSITIVITY, next)
                                    .apply()
                                audioSensitivity = next
                                if (audioService?.state?.value?.isRunning == true) {
                                    stopAudioService()
                                    startAudioService()
                                    Toast.makeText(this@MainActivity, "灵敏度已应用，监听已重启", Toast.LENGTH_SHORT).show()
                                } else {
                                    Toast.makeText(this@MainActivity, "灵敏度已保存", Toast.LENGTH_SHORT).show()
                                }
                            },
                            onNavigateToPair = { navController.navigate("pair") },
                            onBack = { navController.popBackStack() }
                        )
                    }

                    composable("pair") {
                        PairScreen(
                            isPaired = isPaired,
                            pairedComputerName = pairedComputerName,
                            onPair = { code, ip, port ->
                                scope.launch {
                                    val result = doPairing(code, ip, port)
                                    if (result != null) {
                                        pairedToken = result.token
                                        pairedComputerName = result.computerName
                                        checkPermissionsAndStart()
                                    }
                                }
                            },
                            onUnpair = {
                                stopSyncService()
                                stopAudioService()
                                getSharedPreferences("crosshare_transfers", Context.MODE_PRIVATE).edit().clear().commit()
                                prefs.edit()
                                    .remove("paired_token")
                                    .remove("paired_computer_name")
                                    .remove("paired_computer_host")
                                    .remove("paired_computer_port")
                                    .apply()
                                pairedToken = null
                                pairedComputerName = null
                                navController.popBackStack()
                            },
                            onBack = { navController.popBackStack() }
                        )
                    }
                }
            }
        }
    }

    override fun onStart() {
        super.onStart()
        Intent(this, SyncService::class.java).also { intent ->
            bindService(intent, syncServiceConnection, 0)
        }
        Intent(this, AudioStreamService::class.java).also { intent ->
            bindService(intent, audioServiceConnection, 0)
        }
    }

    override fun onStop() {
        super.onStop()
        if (syncBound.value) {
            unbindService(syncServiceConnection)
            syncBound.value = false
        }
        if (audioBound.value) {
            unbindService(audioServiceConnection)
            audioBound.value = false
        }
    }

    private fun checkPermissionsAndStart() {
        val permissions = mutableListOf<String>()

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            // Android 13+
            if (checkSelfPermission(Manifest.permission.READ_MEDIA_IMAGES) != PackageManager.PERMISSION_GRANTED) {
                permissions.add(Manifest.permission.READ_MEDIA_IMAGES)
                if (Build.VERSION.SDK_INT >= 34) permissions.add(Manifest.permission.READ_MEDIA_VISUAL_USER_SELECTED)
            }
            if (checkSelfPermission(Manifest.permission.READ_MEDIA_VIDEO) != PackageManager.PERMISSION_GRANTED) {
                permissions.add(Manifest.permission.READ_MEDIA_VIDEO)
            }
            if (checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
                permissions.add(Manifest.permission.POST_NOTIFICATIONS)
            }
        } else {
            if (checkSelfPermission(Manifest.permission.READ_EXTERNAL_STORAGE) != PackageManager.PERMISSION_GRANTED) {
                permissions.add(Manifest.permission.READ_EXTERNAL_STORAGE)
            }
        }

        if (permissions.isEmpty()) {
            startSyncService()
        } else {
            mediaPermissionLauncher.launch(permissions.toTypedArray())
        }
    }

    private fun startSyncService() {
        serverPrefs().edit().putBoolean(KEY_SYNC_STOPPED, false).apply()
        SyncService.start(this)
        Intent(this, SyncService::class.java).also { intent ->
            bindService(intent, syncServiceConnection, Context.BIND_AUTO_CREATE)
        }
    }

    private fun stopSyncService() {
        if (syncBound.value) {
            unbindService(syncServiceConnection)
            syncBound.value = false
        }
        SyncService.stop(this)
        syncService = null
        serverPrefs().edit().putBoolean(KEY_SYNC_STOPPED, true).apply()
    }

    private fun serverPrefs() = getSharedPreferences("crosshare_server", Context.MODE_PRIVATE)

    /** True when the user paused transfers from the transfers tab. */
    private fun syncStoppedByUser() = serverPrefs().getBoolean(KEY_SYNC_STOPPED, false)

    private fun checkAudioPermissionAndStart() {
        val permissions = mutableListOf<String>()

        if (checkSelfPermission(Manifest.permission.RECORD_AUDIO) != PackageManager.PERMISSION_GRANTED) {
            permissions.add(Manifest.permission.RECORD_AUDIO)
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            permissions.add(Manifest.permission.POST_NOTIFICATIONS)
        }

        if (permissions.isEmpty()) {
            startAudioService()
        } else {
            audioPermissionLauncher.launch(permissions.toTypedArray())
        }
    }

    private fun startAudioService() {
        AudioStreamService.start(this)
        Intent(this, AudioStreamService::class.java).also { intent ->
            bindService(intent, audioServiceConnection, Context.BIND_AUTO_CREATE)
        }
    }

    private fun stopAudioService() {
        if (audioBound.value) {
            unbindService(audioServiceConnection)
            audioBound.value = false
        }
        AudioStreamService.stop(this)
        audioService = null
    }

    /**
     * Send pairing request to the desktop computer.
     */
    private suspend fun doPairing(code: String, computerIp: String, computerPort: Int): PairingResult? {
        return withContext(Dispatchers.IO) {
            var conn: HttpURLConnection? = null
            try {
                val prefs = getSharedPreferences("crosshare_server", Context.MODE_PRIVATE)
                val deviceId = prefs.getString("device_id", null) ?: java.util.UUID.randomUUID().toString().also {
                    prefs.edit().putString("device_id", it).apply()
                }

                val url = URL("http://$computerIp:$computerPort/api/pair")
                conn = url.openConnection() as HttpURLConnection
                conn.requestMethod = "POST"
                conn.setRequestProperty("Content-Type", "application/json")
                conn.doOutput = true
                conn.connectTimeout = 10000
                conn.readTimeout = 10000

                val body = JSONObject().apply {
                    put("pair_code", code)
                    put("device_id", deviceId)
                    put("device_name", Build.MODEL)
                }

                conn.outputStream.use { os ->
                    os.write(body.toString().toByteArray())
                }

                val responseCode = conn.responseCode
                if (responseCode == 200) {
                    val responseBody = conn.inputStream.bufferedReader().readText()
                    val response = JSONObject(responseBody)
                    val token = response.getString("token")
                    val computerName = response.optString("device_name", "Unknown PC")

                    // Save pairing info
                    prefs.edit()
                        .putString("paired_token", token)
                        .putString("paired_computer_name", computerName)
                        .putString("paired_computer_host", computerIp)
                        .putInt("paired_computer_port", computerPort)
                        .apply()

                    withContext(Dispatchers.Main) {
                        Toast.makeText(this@MainActivity, "配对成功: $computerName", Toast.LENGTH_SHORT).show()
                    }
                    PairingResult(token, computerName)
                } else {
                    val errorBody = conn.errorStream?.bufferedReader()?.readText() ?: "Unknown error"
                    withContext(Dispatchers.Main) {
                        Toast.makeText(this@MainActivity, "配对失败: $errorBody", Toast.LENGTH_LONG).show()
                    }
                    null
                }
            } catch (e: Exception) {
                Log.e(TAG, "Pairing failed: ${e.message}", e)
                withContext(Dispatchers.Main) {
                    Toast.makeText(this@MainActivity, "连接失败: ${e.message}", Toast.LENGTH_LONG).show()
                }
                null
            } finally {
                conn?.disconnect()
            }
        }
    }
}
