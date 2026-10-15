package com.crosshare.ui.screens

import org.junit.Assert.assertEquals
import org.junit.Test

class PhotoNavigationTest {
    @Test fun backUnwindsOneLayerAtATime() {
        assertEquals(PhotoBackAction.PREVIEW,photoBackAction(true,false,false,true,true,0,2))
        assertEquals(PhotoBackAction.SELECTION,photoBackAction(false,false,false,true,true,0,2))
        assertEquals(PhotoBackAction.ALBUM_LIST,photoBackAction(false,false,false,false,true,0,2))
        assertEquals(PhotoBackAction.HOME,photoBackAction(false,false,false,false,false,1,0))
        assertEquals(PhotoBackAction.SYSTEM,photoBackAction(false,false,false,false,false,0,0))
    }
    @Test fun tabsFiltersAndDialogsReturnWithoutExitingApp() {
        for(tab in 1..3) assertEquals(PhotoBackAction.HOME,photoBackAction(false,false,false,false,false,tab,0))
        assertEquals(PhotoBackAction.HOME,photoBackAction(false,false,false,false,false,0,2))
        assertEquals(PhotoBackAction.ALBUM_PICKER,photoBackAction(false,true,false,true,true,0,2))
        assertEquals(PhotoBackAction.NEW_ALBUM,photoBackAction(false,false,true,false,false,1,0))
    }
}
