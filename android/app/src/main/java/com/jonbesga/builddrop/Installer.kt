package com.jonbesga.builddrop

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.os.Build as AndroidBuild
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.withContext
import java.io.File

sealed interface InstallResult {
    val buildId: String

    data class Success(override val buildId: String) : InstallResult
    data class Failure(override val buildId: String, val message: String) : InstallResult
}

object ApkInstaller {
    private const val EXTRA_BUILD_ID = "build_id"

    private val _results = MutableSharedFlow<InstallResult>(extraBufferCapacity = 8)
    val results: SharedFlow<InstallResult> = _results

    fun canInstall(context: Context) = context.packageManager.canRequestPackageInstalls()

    /** Streams the APK into a PackageInstaller session and commits it. */
    suspend fun install(context: Context, apk: File, buildId: String) = withContext(Dispatchers.IO) {
        val installer = context.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
        if (AndroidBuild.VERSION.SDK_INT >= AndroidBuild.VERSION_CODES.S) {
            params.setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED)
        }
        params.setSize(apk.length())
        val sessionId = installer.createSession(params)
        installer.openSession(sessionId).use { session ->
            apk.inputStream().use { input ->
                session.openWrite("base.apk", 0, apk.length()).use { out ->
                    input.copyTo(out, 64 * 1024)
                    session.fsync(out)
                }
            }
            val intent = Intent(context, InstallReceiver::class.java).putExtra(EXTRA_BUILD_ID, buildId)
            // Mutable so the system can attach the status extras.
            val pending = PendingIntent.getBroadcast(
                context, sessionId, intent,
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE,
            )
            session.commit(pending.intentSender)
        }
    }

    internal fun onStatus(context: Context, intent: Intent) {
        val buildId = intent.getStringExtra(EXTRA_BUILD_ID).orEmpty()
        when (val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)) {
            PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                @Suppress("DEPRECATION")
                val confirm = if (AndroidBuild.VERSION.SDK_INT >= 33) {
                    intent.getParcelableExtra(Intent.EXTRA_INTENT, Intent::class.java)
                } else {
                    intent.getParcelableExtra(Intent.EXTRA_INTENT)
                }
                if (confirm != null) {
                    context.startActivity(confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                } else {
                    _results.tryEmit(InstallResult.Failure(buildId, "Installer confirmation unavailable"))
                }
            }
            PackageInstaller.STATUS_SUCCESS -> _results.tryEmit(InstallResult.Success(buildId))
            else -> {
                val msg = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE)
                    ?: "Install failed (status $status)"
                _results.tryEmit(InstallResult.Failure(buildId, msg))
            }
        }
    }
}

class InstallReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) = ApkInstaller.onStatus(context, intent)
}
