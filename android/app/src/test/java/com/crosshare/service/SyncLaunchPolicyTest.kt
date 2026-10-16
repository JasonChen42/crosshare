package com.crosshare.service

import org.junit.Assert.assertEquals
import org.junit.Test

class SyncLaunchPolicyTest {
    @Test fun autoStartsForPairedDevices() {
        assertEquals(true, shouldAutoStartSync(paired = true, userStopped = false))
    }

    @Test fun staysOffWhenUserPausedTransfers() {
        assertEquals(false, shouldAutoStartSync(paired = true, userStopped = true))
    }

    @Test fun staysOffBeforePairing() {
        assertEquals(false, shouldAutoStartSync(paired = false, userStopped = false))
        assertEquals(false, shouldAutoStartSync(paired = false, userStopped = true))
    }
}
