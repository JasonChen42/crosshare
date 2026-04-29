package com.crosshare.ui.screens

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.crosshare.ui.theme.*

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PairScreen(
    isPaired: Boolean,
    pairedComputerName: String?,
    onPair: (code: String, computerIp: String, computerPort: Int) -> Unit,
    onUnpair: () -> Unit,
    onBack: () -> Unit
) {
    var pairCode by remember { mutableStateOf("") }
    var computerIp by remember { mutableStateOf("") }
    var computerPort by remember { mutableStateOf("8384") }
    var isPairing by remember { mutableStateOf(false) }
    var errorMessage by remember { mutableStateOf<String?>(null) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(SurfaceDark)
            .padding(20.dp)
    ) {
        // Top bar
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = onBack) {
                Icon(Icons.AutoMirrored.Outlined.ArrowBack, "返回", tint = TextPrimary)
            }
            Spacer(modifier = Modifier.width(8.dp))
            Text("设备配对", style = MaterialTheme.typography.headlineMedium, color = TextPrimary)
        }

        Spacer(modifier = Modifier.height(32.dp))

        if (isPaired && pairedComputerName != null) {
            // Already paired - show status
            Card(
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(20.dp),
                colors = CardDefaults.cardColors(containerColor = Success.copy(alpha = 0.1f))
            ) {
                Column(
                    modifier = Modifier.padding(24.dp),
                    horizontalAlignment = Alignment.CenterHorizontally
                ) {
                    Icon(
                        Icons.Outlined.Link,
                        contentDescription = null,
                        tint = Success,
                        modifier = Modifier.size(48.dp)
                    )
                    Spacer(modifier = Modifier.height(16.dp))
                    Text(
                        "已配对",
                        style = MaterialTheme.typography.titleLarge,
                        color = Success
                    )
                    Spacer(modifier = Modifier.height(8.dp))
                    Text(
                        "电脑: $pairedComputerName",
                        style = MaterialTheme.typography.bodyLarge,
                        color = TextPrimary
                    )
                }
            }

            Spacer(modifier = Modifier.height(24.dp))

            OutlinedButton(
                onClick = onUnpair,
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                colors = ButtonDefaults.outlinedButtonColors(contentColor = Error)
            ) {
                Text("取消配对")
            }
        } else {
            // Not paired - show pairing form
            Text(
                "输入电脑上显示的配对信息",
                style = MaterialTheme.typography.bodyLarge,
                color = TextSecondary
            )

            Spacer(modifier = Modifier.height(24.dp))

            // Computer IP
            OutlinedTextField(
                value = computerIp,
                onValueChange = { computerIp = it },
                label = { Text("电脑 IP 地址") },
                placeholder = { Text("例如: 192.168.1.100") },
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                singleLine = true,
                keyboardOptions = KeyboardOptions(
                    keyboardType = KeyboardType.Uri,
                    imeAction = ImeAction.Next
                ),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedBorderColor = Primary,
                    unfocusedBorderColor = SurfaceLight,
                    focusedTextColor = TextPrimary,
                    unfocusedTextColor = TextPrimary,
                    focusedLabelColor = Primary,
                    unfocusedLabelColor = TextSecondary,
                    focusedPlaceholderColor = TextMuted,
                    unfocusedPlaceholderColor = TextMuted
                )
            )

            Spacer(modifier = Modifier.height(12.dp))

            // Computer port
            OutlinedTextField(
                value = computerPort,
                onValueChange = { computerPort = it },
                label = { Text("端口 (默认 8384)") },
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                singleLine = true,
                keyboardOptions = KeyboardOptions(
                    keyboardType = KeyboardType.Number,
                    imeAction = ImeAction.Next
                ),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedBorderColor = Primary,
                    unfocusedBorderColor = SurfaceLight,
                    focusedTextColor = TextPrimary,
                    unfocusedTextColor = TextPrimary,
                    focusedLabelColor = Primary,
                    unfocusedLabelColor = TextSecondary,
                )
            )

            Spacer(modifier = Modifier.height(12.dp))

            // Pair code
            OutlinedTextField(
                value = pairCode,
                onValueChange = { if (it.length <= 6) pairCode = it },
                label = { Text("6位配对码") },
                placeholder = { Text("输入电脑上的配对码") },
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                singleLine = true,
                textStyle = LocalTextStyle.current.copy(
                    textAlign = TextAlign.Center,
                    fontSize = 24.sp,
                    fontWeight = FontWeight.Bold,
                    letterSpacing = 8.sp
                ),
                keyboardOptions = KeyboardOptions(
                    keyboardType = KeyboardType.Number,
                    imeAction = ImeAction.Done
                ),
                keyboardActions = KeyboardActions(
                    onDone = {
                        if (pairCode.length == 6 && computerIp.isNotBlank()) {
                            onPair(pairCode, computerIp, computerPort.toIntOrNull() ?: 8384)
                        }
                    }
                ),
                colors = OutlinedTextFieldDefaults.colors(
                    focusedBorderColor = Primary,
                    unfocusedBorderColor = SurfaceLight,
                    focusedTextColor = TextPrimary,
                    unfocusedTextColor = TextPrimary,
                    focusedLabelColor = Primary,
                    unfocusedLabelColor = TextSecondary,
                    focusedPlaceholderColor = TextMuted,
                    unfocusedPlaceholderColor = TextMuted
                )
            )

            // Error message
            errorMessage?.let { err ->
                Spacer(modifier = Modifier.height(12.dp))
                Text(err, color = Error, style = MaterialTheme.typography.bodyMedium)
            }

            Spacer(modifier = Modifier.height(24.dp))

            // Pair button
            Button(
                onClick = {
                    if (pairCode.length == 6 && computerIp.isNotBlank()) {
                        onPair(pairCode, computerIp, computerPort.toIntOrNull() ?: 8384)
                    } else {
                        errorMessage = "请填写完整的配对信息"
                    }
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(52.dp),
                shape = RoundedCornerShape(12.dp),
                enabled = pairCode.length == 6 && computerIp.isNotBlank()
            ) {
                Text("配对", style = MaterialTheme.typography.titleMedium)
            }

            Spacer(modifier = Modifier.height(32.dp))

            // Help text
            Card(
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
                colors = CardDefaults.cardColors(containerColor = Info.copy(alpha = 0.1f))
            ) {
                Column(modifier = Modifier.padding(16.dp)) {
                    Text("如何配对？", style = MaterialTheme.typography.titleMedium, color = Info)
                    Spacer(modifier = Modifier.height(8.dp))
                    Text(
                        "1. 在电脑上启动 CrossHare\n" +
                        "2. 右键点击系统托盘图标\n" +
                        "3. 查看「配对码」\n" +
                        "4. 输入电脑的 IP 地址和配对码",
                        style = MaterialTheme.typography.bodyMedium,
                        color = TextSecondary,
                        lineHeight = 22.sp
                    )
                }
            }
        }
    }
}
