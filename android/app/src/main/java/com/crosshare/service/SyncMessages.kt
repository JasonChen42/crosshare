package com.crosshare.service

/**
 * User-facing copy for phone → desktop upload results. Pure string building
 * keeps it unit-testable without Android runtime dependencies.
 */
internal object SyncMessages {
    fun savedNotice(savedCount: Int, port: Int): String =
        "已在电脑保存 $savedCount 项 · 电脑浏览器打开 http://127.0.0.1:$port/"
}
