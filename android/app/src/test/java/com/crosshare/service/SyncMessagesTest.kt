package com.crosshare.service

import org.junit.Assert.assertEquals
import org.junit.Test

class SyncMessagesTest {
    @Test fun savedNoticePointsAtTheLoopbackLibrary() {
        assertEquals(
            "已在电脑保存 3 项 · 电脑浏览器打开 http://127.0.0.1:8384/",
            SyncMessages.savedNotice(3, 8384),
        )
        assertEquals(
            "已在电脑保存 1 项 · 电脑浏览器打开 http://127.0.0.1:9000/",
            SyncMessages.savedNotice(1, 9000),
        )
    }
}
