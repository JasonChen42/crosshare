package com.crosshare.data.repository

import com.crosshare.data.db.MediaEntity
import java.time.Instant
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test

class PhotoPagerTest {
    private fun local(i: Int) = MediaEntity(id=i.toLong(),mediaStoreId=i.toLong(),contentHash="hash-$i",fileName="$i.jpg",filePath="/Pictures/$i.jpg",fileSize=100,mimeType="image/jpeg",mediaType="photo",dateTaken=200000L-i*1000)
    private fun remote(i: Int) = LibraryClient.Asset("hash-$i","$i.jpg",100,"photo",Instant.ofEpochMilli(200000L-i*1000).toString(),false)
    @Test fun mergedPagesAreBoundedOrderedDeduplicatedAndRevisitable() = runBlocking {
        val locals=(0 until 200 step 2).map(::local)
        val remotes=(0 until 200).map(::remote)
        val pager=PhotoPager({offset,_-> locals.drop(offset).take(48)}, {offset,_->remotes.drop(offset).take(48)}, {hashes->locals.filter{it.contentHash in hashes}})
        var cursor=PhotoCursor();val seen=mutableSetOf<String>();val output=mutableListOf<String>()
        do {
            val start=cursor;val before=seen.toSet()
            val page=pager.load(cursor,0,null,0,true,before)
            assertTrue(page.photos.size<=48)
            assertEquals(page,pager.load(start,0,null,0,true,before))
            assertTrue(page.photos.none{it.hash in seen})
            output.addAll(page.photos.map{it.hash});seen.addAll(page.photos.map{it.hash});cursor=page.next
        } while(page.hasNext)
        assertEquals((0 until 200).map{"hash-$it"},output)
    }
    @Test fun localOnlyNeverContactsComputerAndExactLastPageStops() = runBlocking {
        val locals=(0 until 48).map(::local)
        val pager=PhotoPager({offset,_->locals.drop(offset).take(48)}, {_,_->error("Must not contact desktop")}, {emptyList()})
        val page=pager.load(PhotoCursor(),1,null,0,false,emptySet())
        assertEquals(48,page.photos.size);assertFalse(page.hasNext)
    }
}
