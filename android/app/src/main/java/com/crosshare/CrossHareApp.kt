package com.crosshare

import android.app.Application
import com.crosshare.data.db.AppDatabase

class CrossHareApp : Application() {

    val database: AppDatabase by lazy { AppDatabase.getInstance(this) }

    override fun onCreate() {
        super.onCreate()
    }
}
