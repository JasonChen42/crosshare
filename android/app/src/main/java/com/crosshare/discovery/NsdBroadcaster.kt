package com.crosshare.discovery

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import android.util.Log

/**
 * Broadcasts this device's CrossHare service via mDNS/NSD so the desktop can discover it.
 */
class NsdBroadcaster(private val context: Context) {

    companion object {
        private const val TAG = "NsdBroadcaster"
        private const val SERVICE_TYPE = "_crosshare._tcp."
        private const val SERVICE_NAME_PREFIX = "CrossHare-"
    }

    private var nsdManager: NsdManager? = null
    private var registered = false

    private val registrationListener = object : NsdManager.RegistrationListener {
        override fun onServiceRegistered(serviceInfo: NsdServiceInfo) {
            Log.i(TAG, "mDNS service registered: ${serviceInfo.serviceName}")
            registered = true
        }

        override fun onRegistrationFailed(serviceInfo: NsdServiceInfo, errorCode: Int) {
            Log.e(TAG, "mDNS registration failed: error $errorCode")
            registered = false
        }

        override fun onServiceUnregistered(serviceInfo: NsdServiceInfo) {
            Log.i(TAG, "mDNS service unregistered: ${serviceInfo.serviceName}")
            registered = false
        }

        override fun onUnregistrationFailed(serviceInfo: NsdServiceInfo, errorCode: Int) {
            Log.e(TAG, "mDNS unregistration failed: error $errorCode")
        }
    }

    /**
     * Start broadcasting the CrossHare service on the given port.
     */
    fun startBroadcast(port: Int, deviceId: String) {
        if (registered) {
            Log.w(TAG, "Already broadcasting, skipping")
            return
        }

        nsdManager = context.getSystemService(Context.NSD_SERVICE) as NsdManager

        val serviceInfo = NsdServiceInfo().apply {
            serviceName = SERVICE_NAME_PREFIX + android.os.Build.MODEL.replace(" ", "-")
            serviceType = SERVICE_TYPE
            setPort(port)

            // Add TXT records with device metadata
            setAttribute("id", deviceId)
            setAttribute("name", android.os.Build.MODEL)
            setAttribute("api", "1")
        }

        try {
            nsdManager?.registerService(serviceInfo, NsdManager.PROTOCOL_DNS_SD, registrationListener)
            Log.i(TAG, "Registering mDNS service on port $port")
        } catch (e: Exception) {
            Log.e(TAG, "Failed to register mDNS service: ${e.message}", e)
        }
    }

    /**
     * Stop broadcasting the service.
     */
    fun stopBroadcast() {
        if (!registered) return

        try {
            nsdManager?.unregisterService(registrationListener)
        } catch (e: Exception) {
            Log.w(TAG, "Error unregistering mDNS: ${e.message}")
        }
        registered = false
    }

    fun isRegistered(): Boolean = registered
}
