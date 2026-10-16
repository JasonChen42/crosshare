package com.crosshare.service

/**
 * Decides whether opening the app should bring the sync service back up. Pure
 * so the launch policy stays unit-testable without Android runtime state.
 */
internal fun shouldAutoStartSync(paired: Boolean, userStopped: Boolean): Boolean =
    paired && !userStopped
