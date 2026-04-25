package com.crosshare.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import com.crosshare.service.AudioStreamService
import com.crosshare.ui.theme.*

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    isPaired: Boolean,
    pairedComputerName: String?,
    audioSensitivity: String,
    onAudioSensitivityChange: (String) -> Unit,
    onNavigateToPair: () -> Unit,
    onBack: () -> Unit
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
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = onBack) {
                Icon(Icons.Outlined.ArrowBack, "返回", tint = TextPrimary)
            }
            Spacer(modifier = Modifier.width(8.dp))
            Text("设置", style = MaterialTheme.typography.headlineMedium, color = TextPrimary)
        }

        Spacer(modifier = Modifier.height(24.dp))

        // Connection section
        SectionHeader("连接")

        SettingsItem(
            icon = Icons.Outlined.Link,
            title = "设备配对",
            subtitle = if (isPaired) "已配对: $pairedComputerName" else "未配对",
            onClick = onNavigateToPair
        )

        Spacer(modifier = Modifier.height(16.dp))

        AudioSensitivityCard(
            value = audioSensitivity,
            onChange = onAudioSensitivityChange
        )

        Spacer(modifier = Modifier.height(24.dp))

        // About section
        SectionHeader("关于")

        SettingsItem(
            icon = Icons.Outlined.Info,
            title = "CrossHare",
            subtitle = "版本 1.0.0",
            onClick = {}
        )

        SettingsItem(
            icon = Icons.Outlined.Code,
            title = "开源协议",
            subtitle = "MIT License",
            onClick = {}
        )

        Spacer(modifier = Modifier.height(24.dp))

        // Info card
        Card(
            modifier = Modifier.fillMaxWidth(),
            shape = RoundedCornerShape(16.dp),
            colors = CardDefaults.cardColors(containerColor = SurfaceMedium)
        ) {
            Column(modifier = Modifier.padding(20.dp)) {
                Text(
                    "工作原理",
                    style = MaterialTheme.typography.titleMedium,
                    color = TextPrimary
                )
                Spacer(modifier = Modifier.height(8.dp))
                Text(
                    "CrossHare 会把电脑作为接收端服务器。" +
                    "手机配对后保存电脑地址和令牌，点击立即同步时主动上传新照片和视频。" +
                    "所有文件按拍摄日期自动归档到电脑上的目标文件夹中。",
                    style = MaterialTheme.typography.bodyMedium,
                    color = TextSecondary,
                    lineHeight = MaterialTheme.typography.bodyMedium.lineHeight
                )
            }
        }
    }
}

@Composable
fun AudioSensitivityCard(
    value: String,
    onChange: (String) -> Unit
) {
    val options = listOf(
        Triple(
            AudioStreamService.AUDIO_SENSITIVITY_STRICT,
            "低敏",
            "更严格，尽量忽略键盘声和环境噪声"
        ),
        Triple(
            AudioStreamService.AUDIO_SENSITIVITY_BALANCED,
            "平衡",
            "默认建议档，正常说话容易触发，误触发更少"
        ),
        Triple(
            AudioStreamService.AUDIO_SENSITIVITY_SENSITIVE,
            "高敏",
            "更容易触发，适合轻声说话，但更容易误检"
        )
    )

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(16.dp),
        colors = CardDefaults.cardColors(containerColor = SurfaceMedium)
    ) {
        Column(modifier = Modifier.padding(20.dp)) {
            Text(
                "语音触发灵敏度",
                style = MaterialTheme.typography.titleMedium,
                color = TextPrimary
            )
            Spacer(modifier = Modifier.height(8.dp))
            Text(
                "当前手机端会先做本地判断，只有通过门槛才上传到电脑。",
                style = MaterialTheme.typography.bodyMedium,
                color = TextSecondary
            )
            Spacer(modifier = Modifier.height(12.dp))

            options.forEach { (key, title, subtitle) ->
                val selected = value == key
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(12.dp))
                        .clickable { onChange(key) }
                        .background(
                            if (selected) Primary.copy(alpha = 0.14f) else SurfaceDark.copy(alpha = 0.22f)
                        )
                        .padding(14.dp),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    RadioButton(
                        selected = selected,
                        onClick = { onChange(key) },
                        colors = RadioButtonDefaults.colors(selectedColor = Primary)
                    )
                    Spacer(modifier = Modifier.width(12.dp))
                    Column(modifier = Modifier.weight(1f)) {
                        Text(title, style = MaterialTheme.typography.titleSmall, color = TextPrimary)
                        Text(subtitle, style = MaterialTheme.typography.bodyMedium, color = TextSecondary)
                    }
                }
                Spacer(modifier = Modifier.height(10.dp))
            }
        }
    }
}

@Composable
fun SectionHeader(title: String) {
    Text(
        title,
        style = MaterialTheme.typography.labelLarge,
        color = Primary,
        modifier = Modifier.padding(bottom = 8.dp, start = 4.dp)
    )
}

@Composable
fun SettingsItem(
    icon: androidx.compose.ui.graphics.vector.ImageVector,
    title: String,
    subtitle: String,
    onClick: () -> Unit
) {
    Card(
        onClick = onClick,
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 4.dp),
        shape = RoundedCornerShape(12.dp),
        colors = CardDefaults.cardColors(containerColor = SurfaceMedium)
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Icon(
                icon,
                contentDescription = null,
                tint = TextSecondary,
                modifier = Modifier.size(24.dp)
            )
            Spacer(modifier = Modifier.width(16.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(title, style = MaterialTheme.typography.titleMedium, color = TextPrimary)
                Text(subtitle, style = MaterialTheme.typography.bodyMedium, color = TextSecondary)
            }
            Icon(
                Icons.Outlined.ChevronRight,
                contentDescription = null,
                tint = TextMuted
            )
        }
    }
}
