package com.jonbesga.builddrop

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build as AndroidBuild
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

/** Persistent one-shot jobs allow a ten-minute target without a continuously running service. */
object BuildNotifications {
    private const val JOB_ID = 1001
    private const val CHANNEL = "new_builds"
    private const val INTERVAL_MS = 10 * 60 * 1000L

    fun schedule(context: Context, replace: Boolean = false) {
        val scheduler = context.getSystemService(JobScheduler::class.java)
        if (!replace && scheduler.getPendingJob(JOB_ID) != null) return
        scheduler.schedule(
            JobInfo.Builder(JOB_ID, ComponentName(context, BuildPollService::class.java))
                .setMinimumLatency(INTERVAL_MS)
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setPersisted(true)
                .build()
        )
    }

    /** Shared by foreground refresh and background polling; persist separately for each server. */
    @Synchronized
    fun record(context: Context, server: String, builds: List<Build>) {
        val prefs = context.getSharedPreferences("build_notifications", Context.MODE_PRIVATE)
        val key = server.trim().trimEnd('/')
        val ready = builds.filter { it.isInstallable }
        val seen = prefs.getStringSet(key, null)
        if (seen != null) {
            ready.filter { it.id !in seen }.forEach { notify(context, key, it) }
        }
        // Keep the history so a build disappearing and reappearing doesn't alert twice.
        prefs.edit().putStringSet(key, (seen.orEmpty() + ready.map { it.id }).toSet()).commit()
    }

    private fun notify(context: Context, server: String, build: Build) {
        val manager = context.getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel(CHANNEL, "New builds", NotificationManager.IMPORTANCE_DEFAULT))
        if (AndroidBuild.VERSION.SDK_INT >= 33 &&
            context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) return
        if (!NotificationManagerCompat.from(context).areNotificationsEnabled()) return
        val open = PendingIntent.getActivity(
            context, 0,
            Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val notification = NotificationCompat.Builder(context, CHANNEL)
            .setSmallIcon(R.drawable.ic_build_notification)
            .setContentTitle("${build.project} build ready")
            .setContentText(listOf(build.versionName.takeIf { it.isNotEmpty() }?.let { "v$it" }, build.branch, build.commit)
                .filterNotNull().filter { it.isNotEmpty() }.joinToString(" · ").ifEmpty { "Tap to download and install" })
            .setContentIntent(open)
            .setAutoCancel(true)
            .build()
        manager.notify("$server/${build.id}", 0, notification)
    }
}

class BuildPollService : JobService() {
    private var scope: CoroutineScope? = null

    override fun onStartJob(params: JobParameters): Boolean {
        val jobScope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
        scope = jobScope
        jobScope.launch {
            try {
                val server = getSharedPreferences("settings", Context.MODE_PRIVATE)
                    .getString("server_url", null) ?: BuildConfig.DEFAULT_SERVER_URL
                BuildNotifications.record(this@BuildPollService, server, BuildServerApi(server).listBuilds())
            } catch (e: CancellationException) {
                throw e
            } catch (_: Exception) {
                // An unreachable server is normal off VPN; try again at the next check.
            }
            jobFinished(params, false)
            BuildNotifications.schedule(this@BuildPollService, replace = true)
            jobScope.cancel()
            scope = null
        }
        return true
    }

    override fun onStopJob(params: JobParameters): Boolean {
        scope?.cancel()
        scope = null
        return true
    }

    override fun onDestroy() {
        scope?.cancel()
        super.onDestroy()
    }
}
