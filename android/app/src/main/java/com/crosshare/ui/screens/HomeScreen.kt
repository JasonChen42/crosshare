package com.crosshare.ui.screens

import androidx.compose.animation.*
import androidx.compose.animation.core.*
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.crosshare.service.AudioStreamService
import com.crosshare.service.SyncService
import com.crosshare.ui.theme.*
import java.util.Locale

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(
    serviceState: SyncService.ServiceState,
    audioState: AudioStreamService.StreamState,
    pairedComputerName: String?,
    onStartService: () -> Unit,
    onStopService: () -> Unit,
    onStartAudio: () -> Unit,
    onStopAudio: () -> Unit,
    onRescan: () -> Unit,
    onSyncNow: () -> Unit,
    onNavigateToSettings: () -> Unit
) {
    val scrollState = rememberScrollState()

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(SurfaceDark)
            .verticalScroll(scrollState)
            .padding(20.dp)
    ) {
        // Top bar
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically
        ) {
            Text(
                "CrossHare",
                style = MaterialTheme.typography.headlineLarge,
                color = TextPrimary
            )
            IconButton(onClick = onNavigateToSettings) {
                Icon(
                    Icons.Outlined.Settings,
                    contentDescription = "设置",
                    tint = TextSecondary
                )
            }
        }

        Spacer(modifier = Modifier.height(24.dp))

        // Status card
        StatusCard(serviceState, pairedComputerName)

        Spacer(modifier = Modifier.height(16.dp))

        // Action button
        val isRunning = serviceState.isRunning
        Button(
            onClick = { if (isRunning) onStopService() else onStartService() },
            modifier = Modifier
                .fillMaxWidth()
                .height(56.dp),
            shape = RoundedCornerShape(16.dp),
            colors = ButtonDefaults.buttonColors(
                containerColor = if (isRunning) Error else Primary
            )
        ) {
            Icon(
                if (isRunning) Icons.Filled.Stop else Icons.Filled.PlayArrow,
                contentDescription = null,
                modifier = Modifier.size(24.dp)
            )
            Spacer(modifier = Modifier.width(8.dp))
            Text(
                if (isRunning) "停止服务" else "启动同步服务",
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold
            )
        }

        Spacer(modifier = Modifier.height(24.dp))

        // Stats grid
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            StatCard(
                modifier = Modifier.weight(1f),
                icon = Icons.Outlined.Photo,
                label = "总文件",
                value = "${serviceState.totalMedia}",
                color = Primary
            )
            StatCard(
                modifier = Modifier.weight(1f),
                icon = Icons.Outlined.CloudDone,
                label = "已同步",
                value = "${serviceState.syncedMedia}",
                color = Success
            )
        }

        Spacer(modifier = Modifier.height(12.dp))

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            StatCard(
                modifier = Modifier.weight(1f),
                icon = Icons.Outlined.Wifi,
                label = "电脑",
                value = pairedComputerName ?: "未配对",
                color = if (pairedComputerName != null) Success else TextMuted
            )
            StatCard(
                modifier = Modifier.weight(1f),
                icon = Icons.Outlined.Dns,
                label = "同步进度",
                value = if (serviceState.syncing) "${serviceState.syncDone}/${serviceState.syncTotal}" else "就绪",
                color = Info
            )
        }

        Spacer(modifier = Modifier.height(24.dp))

        MicStreamCard(
            state = audioState,
            pairedComputerName = pairedComputerName,
            onStartAudio = onStartAudio,
            onStopAudio = onStopAudio
        )

        Spacer(modifier = Modifier.height(24.dp))

        // Rescan button
        if (isRunning) {
            Button(
                onClick = onSyncNow,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(52.dp),
                shape = RoundedCornerShape(12.dp),
                enabled = !serviceState.indexing && !serviceState.syncing && pairedComputerName != null
            ) {
                if (serviceState.syncing) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(18.dp),
                        strokeWidth = 2.dp,
                        color = TextPrimary
                    )
                    Spacer(modifier = Modifier.width(8.dp))
                    Text("正在同步...")
                } else {
                    Icon(Icons.Outlined.CloudUpload, contentDescription = null, modifier = Modifier.size(18.dp))
                    Spacer(modifier = Modifier.width(8.dp))
                    Text("立即同步到电脑")
                }
            }

            Spacer(modifier = Modifier.height(12.dp))

            OutlinedButton(
                onClick = onRescan,
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                colors = ButtonDefaults.outlinedButtonColors(contentColor = Primary),
                enabled = !serviceState.indexing
            ) {
                if (serviceState.indexing) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(18.dp),
                        strokeWidth = 2.dp,
                        color = Primary
                    )
                    Spacer(modifier = Modifier.width(8.dp))
                    Text("正在扫描...")
                } else {
                    Icon(Icons.Outlined.Refresh, contentDescription = null, modifier = Modifier.size(18.dp))
                    Spacer(modifier = Modifier.width(8.dp))
                    Text("重新扫描媒体")
                }
            }
        }

        // Error display
        serviceState.lastError?.let { error ->
            Spacer(modifier = Modifier.height(16.dp))
            Card(
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                colors = CardDefaults.cardColors(containerColor = Error.copy(alpha = 0.15f))
            ) {
                Row(
                    modifier = Modifier.padding(16.dp),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Icon(Icons.Outlined.ErrorOutline, contentDescription = null, tint = Error)
                    Spacer(modifier = Modifier.width(12.dp))
                    Text(error, color = Error, style = MaterialTheme.typography.bodyMedium)
                }
            }
        }

        audioState.lastError?.let { error ->
            Spacer(modifier = Modifier.height(16.dp))
            Card(
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                colors = CardDefaults.cardColors(containerColor = Error.copy(alpha = 0.15f))
            ) {
                Row(
                    modifier = Modifier.padding(16.dp),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Icon(Icons.Outlined.MicOff, contentDescription = null, tint = Error)
                    Spacer(modifier = Modifier.width(12.dp))
                    Text(error, color = Error, style = MaterialTheme.typography.bodyMedium)
                }
            }
        }
    }
}

@Composable
fun StatusCard(state: SyncService.ServiceState, pairedComputerName: String?) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(20.dp),
        colors = CardDefaults.cardColors(containerColor = SurfaceMedium)
    ) {
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .background(
                    Brush.horizontalGradient(
                        colors = if (state.isRunning) {
                            listOf(Primary.copy(alpha = 0.1f), Secondary.copy(alpha = 0.05f))
                        } else {
                            listOf(SurfaceMedium, SurfaceMedium)
                        }
                    )
                )
                .padding(24.dp)
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                // Animated status indicator
                val statusColor = when {
                    state.syncing -> StatusSyncing
                    state.indexing -> StatusSyncing
                    state.isRunning -> StatusOnline
                    else -> StatusOffline
                }

                val infiniteTransition = rememberInfiniteTransition(label = "pulse")
                val pulseAlpha by infiniteTransition.animateFloat(
                    initialValue = 0.4f,
                    targetValue = 1f,
                    animationSpec = infiniteRepeatable(
                        animation = tween(1000),
                        repeatMode = RepeatMode.Reverse
                    ),
                    label = "pulseAlpha"
                )

                Box(
                    modifier = Modifier
                        .size(14.dp)
                        .clip(CircleShape)
                        .background(
                            statusColor.copy(
                                alpha = if (state.isRunning) pulseAlpha else 1f
                            )
                        )
                )

                Spacer(modifier = Modifier.width(16.dp))

                Column {
                    Text(
                        when {
                            state.syncing -> "正在同步到电脑..."
                            state.indexing -> "正在扫描媒体文件..."
                            state.isRunning -> "服务运行中"
                            else -> "服务未启动"
                        },
                        style = MaterialTheme.typography.titleMedium,
                        color = TextPrimary
                    )
                    Spacer(modifier = Modifier.height(4.dp))
                    Text(
                        when {
                            state.syncing && state.currentFile != null -> state.currentFile
                            state.isRunning && pairedComputerName != null -> "点击立即同步，上传到 $pairedComputerName"
                            state.isRunning -> "先在设置中配对电脑"
                            else -> "点击下方按钮启动同步服务"
                        },
                        style = MaterialTheme.typography.bodyMedium,
                        color = TextSecondary
                    )
                }
            }
        }
    }
}

@Composable
fun MicStreamCard(
    state: AudioStreamService.StreamState,
    pairedComputerName: String?,
    onStartAudio: () -> Unit,
    onStopAudio: () -> Unit
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(20.dp),
        colors = CardDefaults.cardColors(containerColor = SurfaceMedium)
    ) {
        Column(modifier = Modifier.padding(20.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        "手机麦克风",
                        style = MaterialTheme.typography.titleLarge,
                        color = TextPrimary
                    )
                    Spacer(modifier = Modifier.height(4.dp))
                    Text(
                        when {
                            state.isRunning && state.connected -> "检测到说话，正在把这一段语音发到电脑"
                            state.isRunning -> "手机在本地低功耗监听，检测到说话才会发送"
                            pairedComputerName != null -> "点击开始后，手机会常驻监听，只在说话时把语音发到 $pairedComputerName"
                            else -> "先完成电脑配对，再开始省电监听"
                        },
                        style = MaterialTheme.typography.bodyMedium,
                        color = TextSecondary
                    )
                }

                Spacer(modifier = Modifier.width(16.dp))

                Box(
                    modifier = Modifier
                        .size(14.dp)
                        .clip(CircleShape)
                        .background(
                            when {
                                state.isRunning && state.connected -> StatusOnline
                                state.isRunning -> StatusSyncing
                                else -> StatusOffline
                            }
                        )
                )
            }

            Spacer(modifier = Modifier.height(16.dp))

            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(12.dp)
            ) {
                StatCard(
                    modifier = Modifier.weight(1f),
                    icon = Icons.Outlined.Mic,
                    label = "监听格式",
                    value = "${state.sampleRate / 1000}k / ${state.channels}ch",
                    color = Info
                )
                StatCard(
                    modifier = Modifier.weight(1f),
                    icon = Icons.Outlined.GraphicEq,
                    label = "累计上传",
                    value = formatBytes(state.bytesSent),
                    color = if (state.isRunning) Success else TextMuted
                )
            }

            if (state.lastSavedFile != null) {
                Spacer(modifier = Modifier.height(12.dp))
                Text(
                    "最近保存: ${state.lastSavedFile}",
                    style = MaterialTheme.typography.bodyMedium,
                    color = TextSecondary
                )
            }

            Spacer(modifier = Modifier.height(16.dp))

            Button(
                onClick = { if (state.isRunning) onStopAudio() else onStartAudio() },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(52.dp),
                shape = RoundedCornerShape(12.dp),
                enabled = pairedComputerName != null || state.isRunning,
                colors = ButtonDefaults.buttonColors(
                    containerColor = if (state.isRunning) Error else Secondary
                )
            ) {
                Icon(
                    if (state.isRunning) Icons.Filled.Stop else Icons.Filled.Mic,
                    contentDescription = null,
                    modifier = Modifier.size(20.dp)
                )
                Spacer(modifier = Modifier.width(8.dp))
                Text(
                    if (state.isRunning) "停止省电监听" else "开始省电监听",
                    style = MaterialTheme.typography.titleMedium
                )
            }
        }
    }
}

@Composable
fun StatCard(
    modifier: Modifier = Modifier,
    icon: ImageVector,
    label: String,
    value: String,
    color: androidx.compose.ui.graphics.Color
) {
    Card(
        modifier = modifier,
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = SurfaceMedium)
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp)
        ) {
            Icon(
                icon,
                contentDescription = null,
                tint = color,
                modifier = Modifier.size(22.dp)
            )
            Spacer(modifier = Modifier.height(12.dp))
            Text(
                value,
                style = MaterialTheme.typography.headlineMedium,
                color = TextPrimary,
                fontWeight = FontWeight.Bold
            )
            Spacer(modifier = Modifier.height(2.dp))
            Text(
                label,
                style = MaterialTheme.typography.labelMedium,
                color = TextSecondary
            )
        }
    }
}

private fun formatBytes(bytes: Long): String {
    if (bytes < 1024) return "${bytes} B"
    val kb = bytes / 1024.0
    if (kb < 1024) return String.format(Locale.US, "%.1f KB", kb)
    val mb = kb / 1024.0
    return String.format(Locale.US, "%.1f MB", mb)
}
