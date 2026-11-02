package com.jonbesga.buildupdater

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.time.Instant
import kotlin.coroutines.coroutineContext

data class Build(
    val id: String,
    val project: String,
    val task: String,
    val branch: String,
    val commit: String,
    val commitMessage: String,
    val status: String,
    val error: String,
    val createdAt: Instant?,
    val finishedAt: Instant?,
    val apkSize: Long,
    val packageName: String,
    val versionName: String,
    val versionCode: String,
) {
    val isInstallable get() = status == "success"
    val isInProgress get() = status == "queued" || status == "building"

    companion object {
        fun fromJson(o: JSONObject) = Build(
            id = o.getString("id"),
            project = o.optString("project"),
            task = o.optString("task"),
            branch = o.optString("branch"),
            commit = o.optString("commit"),
            commitMessage = o.optString("commit_message"),
            status = o.optString("status"),
            error = o.optString("error"),
            createdAt = o.optInstant("created_at"),
            finishedAt = o.optInstant("finished_at"),
            apkSize = o.optLong("apk_size"),
            packageName = o.optString("package_name"),
            versionName = o.optString("version_name"),
            versionCode = o.optString("version_code"),
        )

        private fun JSONObject.optInstant(key: String): Instant? =
            optString(key).takeIf { it.isNotEmpty() }?.let { runCatching { Instant.parse(it) }.getOrNull() }
    }
}

class BuildServerApi(baseUrl: String) {
    private val base = baseUrl.trim().trimEnd('/')

    suspend fun listBuilds(): List<Build> = withContext(Dispatchers.IO) {
        val conn = open("$base/api/builds")
        try {
            val body = conn.readBody()
            val array = JSONObject(body).getJSONArray("builds")
            List(array.length()) { Build.fromJson(array.getJSONObject(it)) }
        } finally {
            conn.disconnect()
        }
    }

    fun apkUrl(build: Build) = "$base/api/builds/${build.id}/apk"

    /** Downloads the build's APK to [dest], reporting progress in 0..1 (or -1 if unknown size). */
    suspend fun downloadApk(build: Build, dest: File, onProgress: (Float) -> Unit) = withContext(Dispatchers.IO) {
        val conn = open(apkUrl(build), readTimeoutMs = 60_000)
        try {
            if (conn.responseCode !in 200..299) throw IOException("HTTP ${conn.responseCode}")
            val total = conn.contentLengthLong.takeIf { it > 0 } ?: build.apkSize
            val tmp = File(dest.path + ".part")
            conn.inputStream.use { input ->
                tmp.outputStream().use { output ->
                    val buf = ByteArray(64 * 1024)
                    var read = 0L
                    var lastReported = -1
                    while (true) {
                        coroutineContext.ensureActive()
                        val n = input.read(buf)
                        if (n < 0) break
                        output.write(buf, 0, n)
                        read += n
                        if (total > 0) {
                            val pct = (read * 100 / total).toInt()
                            if (pct != lastReported) {
                                lastReported = pct
                                onProgress(read.toFloat() / total)
                            }
                        } else {
                            onProgress(-1f)
                        }
                    }
                }
            }
            if (!tmp.renameTo(dest)) throw IOException("Could not save APK")
        } finally {
            conn.disconnect()
        }
    }

    private fun open(url: String, readTimeoutMs: Int = 15_000): HttpURLConnection {
        val conn = URL(url).openConnection() as HttpURLConnection
        conn.connectTimeout = 10_000
        conn.readTimeout = readTimeoutMs
        return conn
    }

    private fun HttpURLConnection.readBody(): String {
        val code = responseCode
        val stream = if (code in 200..299) inputStream else errorStream
        val text = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
        if (code !in 200..299) {
            val msg = runCatching { JSONObject(text).getString("error") }.getOrNull() ?: text
            throw IOException("HTTP $code: $msg")
        }
        return text
    }
}
