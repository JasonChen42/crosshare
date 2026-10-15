package com.crosshare.data.db

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase

@Database(entities = [MediaEntity::class], version = 3, exportSchema = false)
abstract class AppDatabase : RoomDatabase() {

    abstract fun mediaDao(): MediaDao

    companion object {
        @Volatile private var INSTANCE: AppDatabase? = null

        fun getInstance(context: Context): AppDatabase {
            return INSTANCE
                ?: synchronized(this) {
                    val instance =
                        Room.databaseBuilder(
                                context.applicationContext,
                                AppDatabase::class.java,
                                "crosshare.db",
                            )
                            .addMigrations(
                                object : androidx.room.migration.Migration(1, 2) {
                                    override fun migrate(
                                        db: androidx.sqlite.db.SupportSQLiteDatabase
                                    ) {
                                        db.execSQL(
                                            "ALTER TABLE media_items ADD COLUMN modifiedAt INTEGER NOT NULL DEFAULT 0"
                                        )
                                        db.execSQL(
                                            "DROP INDEX IF EXISTS index_media_items_mediaStoreId"
                                        )
                                        db.execSQL(
                                            "CREATE UNIQUE INDEX IF NOT EXISTS index_media_items_mediaStoreId_mediaType ON media_items(mediaStoreId, mediaType)"
                                        )
                                    }
                                }
                            )
                            .addMigrations(
                                object : androidx.room.migration.Migration(2, 3) {
                                    override fun migrate(
                                        db: androidx.sqlite.db.SupportSQLiteDatabase
                                    ) {
                                        db.execSQL(
                                            "CREATE INDEX IF NOT EXISTS index_media_items_dateTaken_id ON media_items(dateTaken,id)"
                                        )
                                    }
                                }
                            )
                            .build()
                    INSTANCE = instance
                    instance
                }
        }
    }
}
