package com.crosshare.ui.screens

internal enum class PhotoBackAction {
    PREVIEW,
    ALBUM_PICKER,
    NEW_ALBUM,
    SELECTION,
    ALBUM_LIST,
    HOME,
    SYSTEM,
}

internal fun photoBackAction(
    preview: Boolean,
    picker: Boolean,
    newAlbum: Boolean,
    selected: Boolean,
    inAlbum: Boolean,
    tab: Int,
    source: Int,
): PhotoBackAction =
    when {
        preview -> PhotoBackAction.PREVIEW
        picker -> PhotoBackAction.ALBUM_PICKER
        newAlbum -> PhotoBackAction.NEW_ALBUM
        selected -> PhotoBackAction.SELECTION
        inAlbum -> PhotoBackAction.ALBUM_LIST
        tab != 0 || source != 0 -> PhotoBackAction.HOME
        else -> PhotoBackAction.SYSTEM
    }
