package com.crosshare.ui.screens

import android.content.ContentUris
import android.content.Intent
import android.provider.MediaStore
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import coil.compose.AsyncImage
import coil.request.ImageRequest
import com.crosshare.data.db.AppDatabase
import com.crosshare.data.db.MediaEntity
import com.crosshare.data.repository.LibraryClient
import com.crosshare.data.repository.MediaRepository
import com.crosshare.data.repository.PagedPhoto
import com.crosshare.data.repository.PhotoCursor
import com.crosshare.data.repository.PhotoPager
import com.crosshare.service.AudioStreamService
import com.crosshare.service.SyncService
import java.io.File
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

private typealias Photo = PagedPhoto

@OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)
@Composable
fun PhotoHomeScreen(
    serviceState: SyncService.ServiceState,
    audioState: AudioStreamService.StreamState,
    pairedComputerName: String?,
    onStartService: () -> Unit,
    onStopService: () -> Unit,
    onStartAudio: () -> Unit,
    onStopAudio: () -> Unit,
    onRescan: () -> Unit,
    onUpload: (Set<String>) -> Unit,
    onNavigateToSettings: () -> Unit,
) {
    val context = LocalContext.current
    val client = remember { LibraryClient(context.applicationContext) }
    val repository = remember { MediaRepository(context.applicationContext) }
    val dao = remember { AppDatabase.getInstance(context).mediaDao() }
    val folders by remember { dao.folders() }.collectAsState(initial = emptyList())
    var historyPage by remember { mutableIntStateOf(0) }
    val uploaded by
        remember(historyPage) { dao.uploadedPage(historyPage * 48) }
            .collectAsState(initial = emptyList())
    val uploadedTotal by remember { dao.uploadedCount() }.collectAsState(initial = 0)
    val pager = remember {
        PhotoPager(
            { offset, folder -> dao.page(48, offset, folder) },
            { offset, album -> client.assets(offset, album) },
            { hashes -> if (hashes.isEmpty()) emptyList() else dao.matches(hashes) },
        )
    }
    var all by remember { mutableStateOf(emptyList<Photo>()) }
    var pageIndex by remember { mutableIntStateOf(0) }
    var cursors by remember { mutableStateOf(listOf(PhotoCursor())) }
    var pageHashes by remember { mutableStateOf(emptyList<Set<String>>()) }
    val gridState = rememberLazyGridState()
    var tab by rememberSaveable { mutableStateOf(0) }
    var source by rememberSaveable { mutableStateOf(0) }
    var remote by remember { mutableStateOf(emptyList<LibraryClient.Asset>()) }
    var albums by remember { mutableStateOf(emptyList<LibraryClient.Album>()) }
    var queueingAlbum by remember { mutableStateOf(false) }
    var receivePage by remember { mutableIntStateOf(0) }
    var receiveMore by remember { mutableStateOf(false) }
    var deliveries by remember { mutableStateOf(emptyList<LibraryClient.Delivery>()) }
    var selected by remember { mutableStateOf(emptySet<String>()) }
    var preview by remember { mutableStateOf<Photo?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    var connected by remember { mutableStateOf(false) }
    var hasMore by remember { mutableStateOf(false) }
    var refreshVersion by remember { mutableIntStateOf(0) }
    var albumFilter by remember { mutableStateOf<LibraryClient.Album?>(null) }
    var localFolder by remember { mutableStateOf<String?>(null) }
    var newAlbum by remember { mutableStateOf(false) }
    var albumName by remember { mutableStateOf("") }
    var albumPicker by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    suspend fun refresh(target: Int = 0) {
        val version = ++refreshVersion
        val cursor = if (target == 0) PhotoCursor() else cursors.getOrNull(target) ?: return
        val seen = pageHashes.take(target).flatMap { it }.toSet()
        val chosenSource = source
        val chosenFolder = localFolder
        val chosenAlbum = albumFilter?.id ?: 0L
        busy = true
        selected = emptySet()
        try {
            val result =
                withContext(Dispatchers.IO) {
                    pager.load(cursor, chosenSource, chosenFolder, chosenAlbum, client.paired, seen)
                }
            if (version != refreshVersion) return
            all = result.photos
            remote = all.mapNotNull { it.remote }
            cursors =
                if (target == 0) listOf(PhotoCursor(), result.next)
                else cursors.take(target + 1) + result.next
            pageHashes = pageHashes.take(target) + listOf(all.map { it.hash }.toSet())
            pageIndex = target
            hasMore = result.hasNext
            connected = client.paired && result.remoteError == null
            error = result.remoteError
            gridState.scrollToItem(0)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            if (version == refreshVersion) {
                connected = false
                error = "读取照片失败：${e.message}"
            }
        } finally {
            if (version == refreshVersion) busy = false
        }
    }
    val backAction =
        photoBackAction(
            preview != null,
            albumPicker,
            newAlbum,
            selected.isNotEmpty(),
            albumFilter != null || localFolder != null,
            tab,
            source,
        )
    fun goBack() {
        when (backAction) {
            PhotoBackAction.PREVIEW -> preview = null
            PhotoBackAction.ALBUM_PICKER -> albumPicker = false
            PhotoBackAction.NEW_ALBUM -> newAlbum = false
            PhotoBackAction.SELECTION -> selected = emptySet()
            PhotoBackAction.ALBUM_LIST -> {
                refreshVersion++
                busy = false
                albumFilter = null
                localFolder = null
                tab = 1
                source = 0
            }
            PhotoBackAction.HOME -> {
                tab = 0
                source = 0
                scope.launch { refresh() }
            }
            PhotoBackAction.SYSTEM -> Unit
        }
    }
    val canGoBack = backAction != PhotoBackAction.SYSTEM
    BackHandler(enabled = canGoBack) { goBack() }
    LaunchedEffect(pairedComputerName, serviceState.totalMedia, serviceState.indexing) {
        if (!serviceState.indexing) refresh()
    }
    LaunchedEffect(tab, pairedComputerName) {
        if (client.paired && tab == 1)
            try {
                albums = client.albums()
            } catch (e: Exception) {
                error = e.message
            }
    }
    LaunchedEffect(tab, serviceState.isRunning, receivePage) {
        while (true) {
            if (tab == 2 && client.paired) {
                try {
                    val batch = client.deliveries(offset = receivePage * 48, limit = 49)
                    receiveMore = batch.size > 48
                    deliveries = batch.take(48)
                } catch (_: Exception) {}
            }
            delay(5000)
        }
    }
    val localByHash = remember(all) { all.mapNotNull { it.local }.associateBy { it.contentHash } }
    val remoteByHash = remember(all) { all.mapNotNull { it.remote }.associateBy { it.hash } }
    fun localURI(item: MediaEntity) =
        ContentUris.withAppendedId(
            if (item.mediaType == "video") MediaStore.Video.Media.EXTERNAL_CONTENT_URI
            else MediaStore.Images.Media.EXTERNAL_CONTENT_URI,
            item.mediaStoreId,
        )
    fun imageModel(photo: Photo, original: Boolean = false): ImageRequest =
        ImageRequest.Builder(context)
            .data(
                photo.local?.let { localURI(it) }
                    ?: if (client.paired) client.imageURL(photo.hash, original) else null
            )
            .apply {
                if (photo.local == null) addHeader("Authorization", "Bearer ${client.token()}")
            }
            .build()
    val labels = listOf("照片", "相册", "传输", "语音")
    val icons =
        listOf(
            Icons.Outlined.PhotoLibrary,
            Icons.Outlined.CollectionsBookmark,
            Icons.Outlined.Sync,
            Icons.Outlined.Mic,
        )
    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Text(albumFilter?.name ?: localFolder?.let { File(it).name } ?: labels[tab])
                },
                navigationIcon = {
                    if (canGoBack)
                        IconButton(onClick = { goBack() }) {
                            Icon(Icons.AutoMirrored.Outlined.ArrowBack, "返回")
                        }
                },
                actions = {
                    IconButton(
                        onClick = {
                            scope.launch { refresh() }
                            onRescan()
                        },
                        enabled = !busy,
                    ) {
                        Icon(Icons.Outlined.Refresh, "刷新")
                    }
                    IconButton(onClick = onNavigateToSettings) {
                        Icon(Icons.Outlined.Settings, "设备与设置")
                    }
                },
            )
        },
        bottomBar = {
            NavigationBar {
                labels.forEachIndexed { index, label ->
                    NavigationBarItem(
                        selected = tab == index,
                        onClick = {
                            tab = index
                            selected = emptySet()
                            albumFilter = null
                            localFolder = null
                            if (index == 0) scope.launch { refresh() }
                        },
                        icon = { Icon(icons[index], null) },
                        label = { Text(label, maxLines = 1) },
                    )
                }
            }
        },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            if (tab != 3) {
                Text(
                    if (pairedComputerName == null) "本机照片 · 可在设置中连接电脑"
                    else "$pairedComputerName · ${if(connected) "已连接" else "未连接 / 尚未刷新"}",
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                )
                if (busy || serviceState.indexing) LinearProgressIndicator(Modifier.fillMaxWidth())
                (error ?: serviceState.lastError)?.let {
                    Text(
                        it,
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodySmall,
                        modifier = Modifier.padding(16.dp),
                    )
                }
                notice?.let {
                    Text(
                        it,
                        style = MaterialTheme.typography.bodySmall,
                        modifier = Modifier.padding(horizontal = 16.dp),
                    )
                }
                serviceState.savedNotice?.let {
                    SelectionContainer {
                        Text(
                            it,
                            style = MaterialTheme.typography.bodySmall,
                            modifier = Modifier.padding(horizontal = 16.dp),
                        )
                    }
                }
            }
            when (tab) {
                0 -> {
                    Row(
                        Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        listOf("全部", "本机", "电脑").forEachIndexed { i, label ->
                            FilterChip(
                                selected = source == i,
                                onClick = {
                                    source = i
                                    selected = emptySet()
                                    localFolder = null
                                    albumFilter = null
                                    scope.launch { refresh() }
                                },
                                label = { Text(label) },
                            )
                        }
                    }
                    albumFilter?.let { album ->
                        Button(
                            enabled = !queueingAlbum && pairedComputerName != null,
                            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
                            onClick = {
                                scope.launch {
                                    queueingAlbum = true
                                    try {
                                        val count = client.queueAlbum(album.id)
                                        notice =
                                            if (count == 0) "这个相册还没有可下载的图片"
                                            else "已加入 $count 张图片，保存到手机 CrossHare/${album.name} 文件夹"
                                        if (count > 0) onStartService()
                                    } catch (e: Exception) {
                                        error = e.message
                                    } finally {
                                        queueingAlbum = false
                                    }
                                }
                            },
                        ) {
                            Text(if (queueingAlbum) "正在创建任务…" else "下载全部图片（整个相册）")
                        }
                    }
                    if (!serviceState.isRunning)
                        TextButton(
                            onClick = onStartService,
                            modifier = Modifier.padding(horizontal = 8.dp),
                        ) {
                            Text("读取本机照片并开启接收")
                        }
                    if (selected.isNotEmpty()) {
                        Column(
                            Modifier.fillMaxWidth()
                                .background(MaterialTheme.colorScheme.secondaryContainer)
                                .padding(8.dp)
                        ) {
                            Text(
                                "已选择 ${selected.size} 项",
                                style = MaterialTheme.typography.labelMedium,
                            )
                            Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                                TextButton(
                                    enabled =
                                        pairedComputerName != null &&
                                            selected.any { localByHash.containsKey(it) },
                                    onClick = {
                                        val hashes =
                                            selected.filter { localByHash.containsKey(it) }.toSet()
                                        if (!serviceState.isRunning) onStartService()
                                        onUpload(hashes)
                                        notice = "${hashes.size} 项已加入上传队列"
                                        selected = emptySet()
                                    },
                                ) {
                                    Text("上传电脑")
                                }
                                TextButton(
                                    enabled =
                                        pairedComputerName != null &&
                                            !busy &&
                                            selected.any { remoteByHash.containsKey(it) },
                                    onClick = {
                                        scope.launch {
                                            busy = true
                                            try {
                                                for (hash in
                                                    selected.filter {
                                                        remoteByHash.containsKey(it)
                                                    }) client.queue(hash)
                                                notice = "已加入接收队列，请保持接收开启"
                                                selected = emptySet()
                                                onStartService()
                                            } catch (e: Exception) {
                                                error = e.message
                                            } finally {
                                                busy = false
                                            }
                                        }
                                    },
                                ) {
                                    Text("保存手机")
                                }
                                TextButton(onClick = { selected = emptySet() }) { Text("取消") }
                            }
                            if (albums.isNotEmpty())
                                TextButton(onClick = { albumPicker = true }) { Text("加入电脑相册") }
                        }
                    }
                    if (all.isEmpty() && !busy) {
                        Column(
                            Modifier.fillMaxWidth().padding(32.dp),
                            horizontalAlignment = Alignment.CenterHorizontally,
                        ) {
                            Icon(
                                Icons.Outlined.PhotoLibrary,
                                null,
                                modifier = Modifier.size(48.dp),
                                tint = MaterialTheme.colorScheme.primary,
                            )
                            Text("从一张照片开始", style = MaterialTheme.typography.titleLarge)
                            Text(
                                if (source == 2) "从电脑 Web 导入图片后，在这里查看和保存。"
                                else "允许读取照片后浏览本机图库，长按照片可多选上传。",
                                modifier = Modifier.padding(top = 12.dp),
                            )
                        }
                    }
                    LazyVerticalGrid(
                        columns = GridCells.Fixed(3),
                        state = gridState,
                        modifier = Modifier.weight(1f),
                        contentPadding = PaddingValues(4.dp),
                        horizontalArrangement = Arrangement.spacedBy(3.dp),
                        verticalArrangement = Arrangement.spacedBy(3.dp),
                    ) {
                        items(all, key = { it.hash }) { photo ->
                            Column {
                                Box(
                                    Modifier.aspectRatio(1f)
                                        .background(MaterialTheme.colorScheme.surfaceVariant)
                                        .combinedClickable(
                                            onClick = {
                                                if (selected.isEmpty()) preview = photo
                                                else
                                                    selected =
                                                        if (photo.hash in selected)
                                                            selected - photo.hash
                                                        else selected + photo.hash
                                            },
                                            onLongClick = { selected = selected + photo.hash },
                                        )
                                ) {
                                    AsyncImage(
                                        model = imageModel(photo),
                                        contentDescription = photo.name,
                                        contentScale = ContentScale.Crop,
                                        modifier = Modifier.fillMaxSize(),
                                    )
                                    if (photo.hash in selected)
                                        Icon(
                                            Icons.Outlined.CheckCircle,
                                            "已选中",
                                            tint = MaterialTheme.colorScheme.primary,
                                            modifier =
                                                Modifier.align(Alignment.TopEnd)
                                                    .background(MaterialTheme.colorScheme.surface),
                                        )
                                }
                                Text(
                                    if (photo.local != null && photo.remote != null) "本机 · 电脑有记录"
                                    else if (photo.local != null) "本机" else "电脑",
                                    style = MaterialTheme.typography.labelSmall,
                                    modifier = Modifier.padding(4.dp),
                                )
                            }
                        }
                    }
                    Row(
                        Modifier.fillMaxWidth().padding(8.dp),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        TextButton(
                            enabled = pageIndex > 0 && !busy,
                            onClick = { scope.launch { refresh(pageIndex - 1) } },
                        ) {
                            Text("上一页")
                        }
                        Text(
                            "第 ${pageIndex+1} 页 · ${all.size} 项",
                            style = MaterialTheme.typography.labelMedium,
                        )
                        TextButton(
                            enabled = hasMore && !busy,
                            onClick = { scope.launch { refresh(pageIndex + 1) } },
                        ) {
                            Text("下一页")
                        }
                    }
                }
                1 -> {
                    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp)) {
                        item { Text("本机相册", style = MaterialTheme.typography.titleMedium) }
                        items(folders, key = { it.path }) { folder ->
                            ListItem(
                                headlineContent = { Text(File(folder.path).name.ifBlank { "照片" }) },
                                supportingContent = { Text("${folder.count} 项 · 本机") },
                                leadingContent = { Icon(Icons.Outlined.Folder, null) },
                                modifier =
                                    Modifier.combinedClickable(
                                        onClick = {
                                            localFolder = folder.path
                                            albumFilter = null
                                            source = 1
                                            tab = 0
                                            scope.launch { refresh() }
                                        },
                                        onLongClick = {},
                                    ),
                            )
                        }
                        item {
                            HorizontalDivider()
                            Row(
                                Modifier.fillMaxWidth(),
                                horizontalArrangement = Arrangement.SpaceBetween,
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Text("电脑相册", style = MaterialTheme.typography.titleMedium)
                                TextButton(
                                    onClick = { newAlbum = true },
                                    enabled = pairedComputerName != null,
                                ) {
                                    Text("新建")
                                }
                            }
                        }
                        items(albums, key = { it.id }) { album ->
                            ListItem(
                                headlineContent = { Text(album.name) },
                                supportingContent = { Text("${album.count} 项 · 电脑") },
                                leadingContent = { Icon(Icons.Outlined.CollectionsBookmark, null) },
                                modifier =
                                    Modifier.combinedClickable(
                                        onClick = {
                                            albumFilter = album
                                            localFolder = null
                                            source = 2
                                            tab = 0
                                            scope.launch { refresh() }
                                        },
                                        onLongClick = {},
                                    ),
                            )
                        }
                    }
                }
                2 -> {
                    LazyColumn(
                        Modifier.fillMaxSize(),
                        contentPadding = PaddingValues(16.dp),
                        verticalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        item {
                            Text("照片自由往返", style = MaterialTheme.typography.headlineSmall)
                            Text(
                                "接收开启时，电脑发送的照片会自动保存到系统相册。中断的任务会在下次开启后继续。",
                                modifier = Modifier.padding(vertical = 12.dp),
                            )
                            Button(
                                onClick =
                                    if (serviceState.isRunning) onStopService else onStartService
                            ) {
                                Text(if (serviceState.isRunning) "暂停传输" else "开启传输与接收")
                            }
                        }
                        if (serviceState.syncing)
                            item {
                                Text(serviceState.currentFile ?: "准备传输")
                                LinearProgressIndicator(Modifier.fillMaxWidth())
                                Text("${serviceState.syncDone} / ${serviceState.syncTotal}")
                            }
                        item {
                            Text("电脑 → 手机", style = MaterialTheme.typography.titleMedium)
                            Row(
                                Modifier.fillMaxWidth(),
                                horizontalArrangement = Arrangement.SpaceBetween,
                            ) {
                                TextButton(enabled = receivePage > 0, onClick = { receivePage-- }) {
                                    Text("上一页")
                                }
                                Text("第 ${receivePage+1} 页")
                                TextButton(enabled = receiveMore, onClick = { receivePage++ }) {
                                    Text("下一页")
                                }
                            }
                        }
                        if (deliveries.isEmpty()) item { Text("还没有接收任务。在电脑 Web 中选择图片并发送到这台手机。") }
                        items(deliveries, key = { it.id }) { task ->
                            ListItem(
                                headlineContent = { Text(task.name) },
                                supportingContent = {
                                    Text(
                                        if (task.status == "done") "已保存（接收记录）"
                                        else task.error.ifBlank { "等待接收" }
                                    )
                                },
                                leadingContent = {
                                    Icon(
                                        if (task.status == "done") Icons.Outlined.CheckCircle
                                        else Icons.Outlined.Download,
                                        null,
                                    )
                                },
                            )
                        }
                        item {
                            HorizontalDivider()
                            Text("待上传 ${serviceState.pendingUploads} 项")
                            Text("手机 → 电脑", style = MaterialTheme.typography.titleMedium)
                            Row(
                                Modifier.fillMaxWidth(),
                                horizontalArrangement = Arrangement.SpaceBetween,
                            ) {
                                TextButton(enabled = historyPage > 0, onClick = { historyPage-- }) {
                                    Text("上一页")
                                }
                                Text("${historyPage+1} / ${maxOf(1,(uploadedTotal+47)/48)}")
                                TextButton(
                                    enabled = (historyPage + 1) * 48 < uploadedTotal,
                                    onClick = { historyPage++ },
                                ) {
                                    Text("下一页")
                                }
                            }
                            Text("在照片页长按选择图片，然后上传电脑。已上传记录不代表电脑原图仍然存在。")
                        }
                        items(uploaded, key = { it.contentHash }) { item ->
                            ListItem(
                                headlineContent = { Text(item.fileName) },
                                supportingContent = { Text("已上传（历史记录）") },
                                leadingContent = { Icon(Icons.Outlined.Upload, null) },
                            )
                        }
                    }
                }
                3 ->
                    Column(
                        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp)
                    ) {
                        Text("把手机变成电脑的麦克风", style = MaterialTheme.typography.headlineSmall)
                        Spacer(Modifier.height(16.dp))
                        MicStreamCard(audioState, pairedComputerName, onStartAudio, onStopAudio)
                        audioState.lastError?.let {
                            Text(it, color = MaterialTheme.colorScheme.error)
                        }
                        TextButton(onClick = onNavigateToSettings) { Text("麦克风灵敏度与设备设置") }
                    }
            }
        }
    }
    preview?.let { photo ->
        var zoom by remember(photo.hash) { mutableFloatStateOf(1f) }
        var pan by remember(photo.hash) { mutableStateOf(Offset.Zero) }
        Dialog(
            onDismissRequest = { preview = null },
            properties = DialogProperties(usePlatformDefaultWidth = false),
        ) {
            Surface(Modifier.fillMaxSize()) {
                Column(Modifier.fillMaxSize().padding(16.dp)) {
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                        Text("照片详情", style = MaterialTheme.typography.titleLarge)
                        TextButton(onClick = { preview = null }) { Text("关闭") }
                    }
                    Text(photo.name)
                    Box(
                        Modifier.weight(1f).fillMaxWidth().clipToBounds().pointerInput(photo.hash) {
                            detectTransformGestures { _, movement, factor, _ ->
                                zoom = (zoom * factor).coerceIn(1f, 5f)
                                pan = if (zoom == 1f) Offset.Zero else pan + movement
                            }
                        }
                    ) {
                        AsyncImage(
                            model = imageModel(photo, true),
                            contentDescription = photo.name,
                            contentScale = ContentScale.Fit,
                            modifier =
                                Modifier.fillMaxSize()
                                    .graphicsLayer(
                                        scaleX = zoom,
                                        scaleY = zoom,
                                        translationX = pan.x,
                                        translationY = pan.y,
                                    ),
                        )
                    }
                    if (photo.local?.mediaType == "video")
                        TextButton(
                            onClick = {
                                runCatching {
                                        context.startActivity(
                                            Intent(Intent.ACTION_VIEW)
                                                .setDataAndType(
                                                    localURI(photo.local),
                                                    photo.local.mimeType,
                                                )
                                                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                                        )
                                    }
                                    .onFailure { error = "没有可用的视频播放器" }
                            }
                        ) {
                            Text("播放本机视频")
                        }
                    Text(if (photo.local != null) "本机照片可离线查看" else "电脑原图 · 保存后可离线查看")
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        if (photo.local != null)
                            Button(
                                onClick = {
                                    if (!serviceState.isRunning) onStartService()
                                    onUpload(setOf(photo.hash))
                                    notice =
                                        if (serviceState.isRunning) "已加入上传队列"
                                        else "已开启传输并加入上传队列"
                                    preview = null
                                },
                                enabled = pairedComputerName != null,
                            ) {
                                Text(if (serviceState.isRunning) "上传电脑" else "开启传输并上传")
                            }
                        if (photo.remote != null)
                            Button(
                                onClick = {
                                    scope.launch {
                                        try {
                                            client.queue(photo.hash)
                                            onStartService()
                                            notice = "已加入接收队列"
                                            preview = null
                                        } catch (e: Exception) {
                                            error = e.message
                                            preview = null
                                        }
                                    }
                                }
                            ) {
                                Text("保存手机")
                            }
                        if (photo.remote != null)
                            TextButton(
                                onClick = {
                                    scope.launch {
                                        try {
                                            client.favorite(photo.hash, !photo.remote.favorite)
                                            preview = null
                                            refresh()
                                        } catch (e: Exception) {
                                            error = e.message
                                            preview = null
                                        }
                                    }
                                }
                            ) {
                                Text(if (photo.remote.favorite) "取消收藏" else "收藏")
                            }
                    }
                }
            }
        }
    }
    if (newAlbum)
        AlertDialog(
            onDismissRequest = { newAlbum = false },
            title = { Text("新建电脑相册") },
            text = {
                OutlinedTextField(
                    value = albumName,
                    onValueChange = { albumName = it.take(80) },
                    label = { Text("相册名称") },
                )
            },
            confirmButton = {
                TextButton(
                    enabled = albumName.isNotBlank(),
                    onClick = {
                        scope.launch {
                            try {
                                client.request(
                                    "/albums",
                                    org.json.JSONObject().put("name", albumName),
                                )
                                albumName = ""
                                newAlbum = false
                                refresh()
                            } catch (e: Exception) {
                                error = e.message
                                newAlbum = false
                            }
                        }
                    },
                ) {
                    Text("创建")
                }
            },
            dismissButton = { TextButton(onClick = { newAlbum = false }) { Text("取消") } },
        )
    if (albumPicker)
        AlertDialog(
            onDismissRequest = { albumPicker = false },
            title = { Text("加入电脑相册") },
            text = {
                Column {
                    albums.forEach { a ->
                        TextButton(
                            onClick = {
                                scope.launch {
                                    try {
                                        for (hash in
                                            selected.filter { remoteByHash.containsKey(it) }) client
                                            .request(
                                                "/albums/${a.id}",
                                                org.json.JSONObject().put("hash", hash),
                                            )
                                        notice = "已将电脑照片加入相册；本机照片请先上传"
                                        selected = emptySet()
                                        albumPicker = false
                                        refresh()
                                    } catch (e: Exception) {
                                        error = e.message
                                        albumPicker = false
                                    }
                                }
                            }
                        ) {
                            Text(a.name)
                        }
                    }
                }
            },
            confirmButton = { TextButton(onClick = { albumPicker = false }) { Text("关闭") } },
        )
}
